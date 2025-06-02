package voice

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/diamondburned/arikawa/v3/discord"
	"go.uber.org/zap"
)

type AudioMixer interface {
	// Add user audio to buffer with RTP timing info
	AddUserAudioWithRTP(userID discord.UserID, audio []byte, timestamp time.Time, rtpTimestamp uint32, sequence uint16) error

	// Get mixed audio for a time window
	GetMixedAudio(duration time.Duration) ([]byte, error)

	// Get all available mixed audio based on actual RTP timestamps
	// Returns the mixed audio and the actual duration it represents
	GetAllAvailableMixedAudio() ([]byte, time.Duration, error)

	// Get all available mixed audio and immediately flush all buffers
	// This is an atomic operation that ensures buffers are cleared
	GetAllAvailableMixedAudioAndFlush() ([]byte, time.Duration, error)

	// Clear buffers for a user
	ClearUserBuffer(userID discord.UserID)

	// Clear all buffers
	ClearAllBuffers()

	// Get dominant speaker
	GetDominantSpeaker() (discord.UserID, float32) // returns userID, confidence
}

// audioPacket represents a single audio packet with metadata
type audioPacket struct {
	audio        []byte
	timestamp    time.Time
	rtpTimestamp uint32
	sequence     uint16
}

// userStream represents a user's audio stream
type userStream struct {
	mu           sync.RWMutex
	userID       discord.UserID
	packets      map[uint16]*audioPacket // sequence -> packet
	lastUpdate   time.Time
	energyLevel  float32
	rtpBase      uint32 // Base RTP timestamp for unwrapping
	rtpHighBits  uint32 // High bits for RTP unwrapping
	
	// Track cumulative energy for dominant speaker detection
	cumulativeEnergy float64
	packetCount      int
}

type audioMixer struct {
	logger     *zap.Logger
	cfg        *config.VoiceConfig
	streams    sync.Map // map[discord.UserID]*userStream
	sampleRate int
	frameSize  int

	// Performance tracking
	perfMu      sync.RWMutex
	avgMixTime  time.Duration
	fallbackMode bool
}

const (
	maxJitterBufferSize = 500  // Maximum packets in jitter buffer
	maxBufferAge        = 10 * time.Second
)

func NewAudioMixer(logger *zap.Logger, cfg *config.Config) AudioMixer {
	return &audioMixer{
		logger:     logger,
		cfg:        &cfg.Voice,
		sampleRate: OpenAISampleRate, // 24kHz
		frameSize:  OpenAIFrameSize,  // 480 samples (20ms at 24kHz)
	}
}

func (m *audioMixer) AddUserAudioWithRTP(userID discord.UserID, audio []byte, timestamp time.Time, rtpTimestamp uint32, sequence uint16) error {
	if len(audio) == 0 {
		return nil
	}

	// Get or create user stream
	streamInt, _ := m.streams.LoadOrStore(userID, &userStream{
		userID:  userID,
		packets: make(map[uint16]*audioPacket),
		cumulativeEnergy: 0,
		packetCount: 0,
	})
	stream := streamInt.(*userStream)

	stream.mu.Lock()
	defer stream.mu.Unlock()

	// Update metadata
	stream.lastUpdate = timestamp
	stream.energyLevel = m.calculateRMS(audio)
	
	// Update cumulative energy tracking
	stream.cumulativeEnergy += float64(stream.energyLevel)
	stream.packetCount++

	// Handle RTP wraparound
	if len(stream.packets) > 0 {
		// Find a recent packet for comparison
		var recentPacket *audioPacket
		for _, p := range stream.packets {
			if recentPacket == nil || p.sequence > recentPacket.sequence {
				recentPacket = p
			}
		}

		if recentPacket != nil {
			// Check for wraparound
			if rtpTimestamp < recentPacket.rtpTimestamp && (recentPacket.rtpTimestamp-rtpTimestamp) > (1<<31) {
				// Wraparound detected
				stream.rtpHighBits++
			}
		}
	}

	// Store packet
	packet := &audioPacket{
		audio:        audio,
		timestamp:    timestamp,
		rtpTimestamp: rtpTimestamp,
		sequence:     sequence,
	}
	stream.packets[sequence] = packet

	// Clean old packets
	m.cleanOldPackets(stream)

	m.logger.Debug("Added user audio",
		zap.String("user_id", userID.String()),
		zap.Int("audio_size", len(audio)),
		zap.Float32("energy", stream.energyLevel),
		zap.Uint32("rtp", rtpTimestamp),
		zap.Uint16("seq", sequence))

	return nil
}

func (m *audioMixer) cleanOldPackets(stream *userStream) {
	cutoff := time.Now().Add(-maxBufferAge)
	for seq, packet := range stream.packets {
		if packet.timestamp.Before(cutoff) {
			delete(stream.packets, seq)
		}
	}

	// Also limit by count
	if len(stream.packets) > maxJitterBufferSize {
		// Get all packets sorted by sequence
		packets := make([]*audioPacket, 0, len(stream.packets))
		for _, p := range stream.packets {
			packets = append(packets, p)
		}
		sort.Slice(packets, func(i, j int) bool {
			return packets[i].timestamp.Before(packets[j].timestamp)
		})

		// Remove oldest
		toRemove := len(packets) - maxJitterBufferSize
		for i := 0; i < toRemove; i++ {
			delete(stream.packets, packets[i].sequence)
		}
	}
}

func (m *audioMixer) GetMixedAudio(duration time.Duration) ([]byte, error) {
	startTime := time.Now()
	defer func() {
		elapsed := time.Since(startTime)
		m.perfMu.Lock()
		m.avgMixTime = (m.avgMixTime + elapsed) / 2
		m.perfMu.Unlock()
	}()

	// Handle negative or zero duration
	if duration <= 0 {
		return []byte{}, nil
	}

	expectedSamples := int(duration.Seconds() * float64(m.sampleRate))
	expectedBytes := expectedSamples * 2

	// Collect active streams
	activeStreams := m.getActiveStreams()
	if len(activeStreams) == 0 {
		return make([]byte, expectedBytes), nil
	}

	// Find global earliest RTP timestamp
	globalEarliestRTP := m.findGlobalEarliestRTP(activeStreams)

	if len(activeStreams) == 1 {
		// Single user, no mixing needed
		return m.extractUserAudio(activeStreams[0], duration, globalEarliestRTP), nil
	}

	// Multiple users - perform mixing
	return m.mixMultipleStreams(activeStreams, duration, globalEarliestRTP)
}

func (m *audioMixer) getActiveStreams() []*userStream {
	var streams []*userStream
	now := time.Now()
	cutoff := now.Add(-2 * time.Second)

	m.streams.Range(func(key, value interface{}) bool {
		stream := value.(*userStream)
		stream.mu.RLock()
		hasPackets := len(stream.packets) > 0
		isRecent := stream.lastUpdate.After(cutoff)
		stream.mu.RUnlock()

		if hasPackets && isRecent {
			streams = append(streams, stream)
		}
		return true
	})

	return streams
}

func (m *audioMixer) findGlobalEarliestRTP(streams []*userStream) uint32 {
	earliest := uint32(math.MaxUint32)

	for _, stream := range streams {
		stream.mu.RLock()
		for _, packet := range stream.packets {
			if packet.rtpTimestamp < earliest {
				earliest = packet.rtpTimestamp
			}
		}
		stream.mu.RUnlock()
	}

	return earliest
}

func (m *audioMixer) extractUserAudio(stream *userStream, duration time.Duration, globalEarliestRTP uint32) []byte {
	expectedSamples := int(duration.Seconds() * float64(m.sampleRate))
	expectedBytes := expectedSamples * 2

	stream.mu.Lock()
	
	// Get packets sorted by RTP timestamp
	packets := make([]*audioPacket, 0, len(stream.packets))
	for _, p := range stream.packets {
		packets = append(packets, p)
	}
	
	if len(packets) == 0 {
		stream.mu.Unlock()
		return make([]byte, expectedBytes)
	}
	
	sort.Slice(packets, func(i, j int) bool {
		return packets[i].rtpTimestamp < packets[j].rtpTimestamp
	})

	// Calculate front padding
	streamStartRTP := packets[0].rtpTimestamp
	padSamples := 0
	if streamStartRTP > globalEarliestRTP {
		deltaRTP := streamStartRTP - globalEarliestRTP
		padSamples = int(deltaRTP / 2) // Convert 48kHz to 24kHz
		if padSamples > expectedSamples {
			padSamples = expectedSamples
		}
	}

	result := make([]byte, 0, expectedBytes)

	// Add front padding
	if padSamples > 0 {
		result = append(result, make([]byte, padSamples*2)...)
	}

	// Process packets and handle gaps
	for i, packet := range packets {
		if i > 0 {
			prevPacket := packets[i-1]
			expectedDelta := int((packet.rtpTimestamp - prevPacket.rtpTimestamp) / 2)
			actualSamples := len(prevPacket.audio) / 2
			gapSamples := expectedDelta - actualSamples

			if gapSamples > 0 && gapSamples < 2400 { // Cap at 100ms
				// Insert silence for gap
				result = append(result, make([]byte, gapSamples*2)...)
			} else if gapSamples < 0 {
				// Handle overlap by trimming
				trimBytes := -gapSamples * 2
				if trimBytes < len(result) {
					result = result[:len(result)-trimBytes]
				}
			}
		}

		result = append(result, packet.audio...)
	}

	// Remove used packets
	for _, packet := range packets {
		delete(stream.packets, packet.sequence)
	}
	
	stream.mu.Unlock()

	// Adjust to expected size
	if len(result) > expectedBytes {
		// Keep the end (most recent audio)
		result = result[len(result)-expectedBytes:]
	} else if len(result) < expectedBytes {
		// Pad with silence
		padding := make([]byte, expectedBytes-len(result))
		result = append(result, padding...)
	}

	return result
}

func (m *audioMixer) mixMultipleStreams(streams []*userStream, duration time.Duration, globalEarliestRTP uint32) ([]byte, error) {
	expectedSamples := int(duration.Seconds() * float64(m.sampleRate))

	// Extract audio from each stream
	streamAudio := make([][]float32, len(streams))
	for i, stream := range streams {
		audioBytes := m.extractUserAudio(stream, duration, globalEarliestRTP)
		streamAudio[i] = m.bytesToFloat32(audioBytes)
	}

	// Mix using logarithmic algorithm
	mixed := make([]float32, expectedSamples)
	adaptiveWeight := float32(math.Sqrt(1.0 / float64(len(streams))))
	if adaptiveWeight < 0.5 {
		adaptiveWeight = 0.5
	}

	for i := 0; i < expectedSamples; i++ {
		sum := float32(0)
		activeCount := 0

		for _, samples := range streamAudio {
			if i < len(samples) {
				sum += samples[i]
				activeCount++
			}
		}

		if activeCount > 0 {
			// Apply adaptive weight and soft limiting
			absSum := float32(math.Abs(float64(sum)))
			compressionScale := float32(1.0)
			
			if absSum > 0.7 {
				compressionScale = 0.9 / absSum
				if compressionScale < 0.7 {
					compressionScale = 0.7
				}
			}

			mixed[i] = sum * adaptiveWeight * compressionScale
		}
	}

	return m.float32ToBytes(mixed), nil
}

func (m *audioMixer) GetAllAvailableMixedAudio() ([]byte, time.Duration, error) {
	// Find time range of available audio
	var earliestTime, latestTime time.Time
	hasAudio := false

	m.streams.Range(func(key, value interface{}) bool {
		stream := value.(*userStream)
		stream.mu.RLock()
		for _, packet := range stream.packets {
			if !hasAudio {
				earliestTime = packet.timestamp
				latestTime = packet.timestamp
				hasAudio = true
			} else {
				if packet.timestamp.Before(earliestTime) {
					earliestTime = packet.timestamp
				}
				if packet.timestamp.After(latestTime) {
					latestTime = packet.timestamp
				}
			}
		}
		stream.mu.RUnlock()
		return true
	})

	if !hasAudio {
		return []byte{}, 0, nil
	}

	// Calculate duration
	duration := latestTime.Sub(earliestTime) + 20*time.Millisecond

	// Sanity checks
	if duration < 100*time.Millisecond {
		duration = 100 * time.Millisecond
	} else if duration > 30*time.Second {
		duration = 30 * time.Second
	}

	// Get mixed audio
	mixed, err := m.GetMixedAudio(duration)
	return mixed, duration, err
}

func (m *audioMixer) GetAllAvailableMixedAudioAndFlush() ([]byte, time.Duration, error) {
	mixed, duration, err := m.GetAllAvailableMixedAudio()
	if err != nil {
		return nil, 0, err
	}

	m.ClearAllBuffers()
	return mixed, duration, nil
}

func (m *audioMixer) ClearUserBuffer(userID discord.UserID) {
	m.streams.Delete(userID)
}

func (m *audioMixer) ClearAllBuffers() {
	m.streams.Range(func(key, value interface{}) bool {
		stream := value.(*userStream)
		stream.mu.Lock()
		stream.packets = make(map[uint16]*audioPacket)
		stream.cumulativeEnergy = 0
		stream.packetCount = 0
		stream.mu.Unlock()
		return true
	})
}

func (m *audioMixer) GetDominantSpeaker() (discord.UserID, float32) {
	var dominantUser discord.UserID
	var maxCumulativeEnergy float64
	var totalCumulativeEnergy float64
	var activeUsers int

	recentTime := time.Now().Add(-2 * time.Second)

	m.streams.Range(func(key, value interface{}) bool {
		stream := value.(*userStream)
		stream.mu.RLock()
		if stream.lastUpdate.After(recentTime) && stream.packetCount > 0 {
			// Use cumulative energy to find who spoke the most overall
			cumulativeEnergy := stream.cumulativeEnergy
			
			if cumulativeEnergy > maxCumulativeEnergy {
				maxCumulativeEnergy = cumulativeEnergy
				dominantUser = stream.userID
			}
			totalCumulativeEnergy += cumulativeEnergy
			activeUsers++
		}
		stream.mu.RUnlock()
		return true
	})

	confidence := float32(0.0)
	if totalCumulativeEnergy > 0 && activeUsers > 1 {
		// Confidence is the ratio of dominant speaker's cumulative energy to total cumulative energy
		confidence = float32(maxCumulativeEnergy / totalCumulativeEnergy)
	} else if activeUsers == 1 && maxCumulativeEnergy > 0 {
		// Single speaker always has full confidence
		confidence = 1.0
	}

	return dominantUser, confidence
}

func (m *audioMixer) calculateRMS(audio []byte) float32 {
	if len(audio) < 2 {
		return 0.0
	}

	var sum float64
	sampleCount := len(audio) / 2

	for i := 0; i < len(audio)-1; i += 2 {
		sample := int16(uint16(audio[i]) | (uint16(audio[i+1]) << 8))
		sampleFloat := float64(sample) / 32768.0
		sum += sampleFloat * sampleFloat
	}

	if sampleCount == 0 {
		return 0.0
	}

	return float32(math.Sqrt(sum / float64(sampleCount)))
}

func (m *audioMixer) bytesToFloat32(audio []byte) []float32 {
	samples := make([]float32, len(audio)/2)
	for i := 0; i+1 < len(audio); i += 2 {
		sample := int16(uint16(audio[i]) | (uint16(audio[i+1]) << 8))
		samples[i/2] = float32(sample) / 32768.0
	}
	return samples
}

func (m *audioMixer) float32ToBytes(samples []float32) []byte {
	result := make([]byte, len(samples)*2)
	for i, sample := range samples {
		scaled := sample * 32768.0
		if scaled > 32767 {
			scaled = 32767
		} else if scaled < -32768 {
			scaled = -32768
		}

		sample16 := int16(scaled)
		result[i*2] = byte(sample16)
		result[i*2+1] = byte(sample16 >> 8)
	}
	return result
}