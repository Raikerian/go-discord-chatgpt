package voice

import (
	"math"
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

	// Get user audio for debugging
	GetUserAudioForDebug(userID discord.UserID, duration time.Duration) []byte

	// Get all active users for debugging
	GetActiveUsersForDebug() []discord.UserID
}

// userStream represents a single user's audio processing pipeline
type userStream struct {
	userID           discord.UserID
	lastUpdate       time.Time
	lastRTPTimestamp uint32
	lastSequence     uint16
	energyLevel      float32
	
	// Jitter buffer to handle packet reordering and timing
	jitterBuffer *jitterBuffer
}

// jitterBuffer handles packet reordering and provides smooth playback
type jitterBuffer struct {
	packets map[uint16]*audioPacketData // sequence -> packet
	mu      sync.Mutex
}

type audioPacketData struct {
	audio        []byte
	rtpTimestamp uint32
	sequence     uint16
	timestamp    time.Time
}

type audioMixer struct {
	logger       *zap.Logger
	cfg          *config.VoiceConfig
	userStreams  sync.Map // map[discord.UserID]*userStream
	sampleRate   int
	frameSize    int
	
	// Synchronization
	synchronizer *streamSynchronizer
	
	// Performance tracking
	avgMixDuration time.Duration
	fallbackMode   bool
}

// streamSynchronizer manages cross-stream synchronization
type streamSynchronizer struct {
	baseTime      time.Time
	streamOffsets sync.Map // map[discord.UserID]int64 - sample offsets
	sampleRate    int
}

func NewAudioMixer(logger *zap.Logger, cfg *config.Config) AudioMixer {
	return &audioMixer{
		logger:     logger,
		cfg:        &cfg.Voice,
		sampleRate: OpenAISampleRate, // 24kHz
		frameSize:  OpenAIFrameSize,  // 480 samples (20ms at 24kHz)
		synchronizer: &streamSynchronizer{
			baseTime:   time.Now(),
			sampleRate: OpenAISampleRate,
		},
	}
}

func (m *audioMixer) AddUserAudioWithRTP(userID discord.UserID, audio []byte, timestamp time.Time, rtpTimestamp uint32, sequence uint16) error {
	if len(audio) == 0 {
		return nil
	}

	// Get or create user stream
	streamInt, _ := m.userStreams.LoadOrStore(userID, m.createUserStream(userID))
	stream := streamInt.(*userStream)
	
	// Update stream metadata
	stream.lastUpdate = timestamp
	
	// Check for packet loss or reordering
	if sequence != 0 && stream.lastSequence != 0 {
		expectedSeq := stream.lastSequence + 1
		if sequence != expectedSeq {
			if sequence < stream.lastSequence {
				m.logger.Debug("Out of order packet",
					zap.String("user_id", userID.String()),
					zap.Uint16("expected", expectedSeq),
					zap.Uint16("received", sequence))
			} else {
				lost := sequence - expectedSeq
				m.logger.Debug("Packet loss detected",
					zap.String("user_id", userID.String()),
					zap.Uint16("lost_packets", lost))
			}
		}
	}
	
	// Update synchronization offset
	m.synchronizer.calculateOffset(userID, rtpTimestamp, timestamp)
	
	// Calculate energy level (RMS)
	energyLevel := m.calculateRMS(audio)
	stream.energyLevel = energyLevel
	stream.lastRTPTimestamp = rtpTimestamp
	stream.lastSequence = sequence
	
	// Add to jitter buffer
	packet := &audioPacketData{
		audio:        audio,
		rtpTimestamp: rtpTimestamp,
		sequence:     sequence,
		timestamp:    timestamp,
	}
	stream.jitterBuffer.insert(packet)
	
	m.logger.Debug("Added user audio",
		zap.String("user_id", userID.String()),
		zap.Int("audio_size", len(audio)),
		zap.Float32("energy_level", energyLevel),
		zap.Uint32("rtp_timestamp", rtpTimestamp),
		zap.Uint16("sequence", sequence))

	return nil
}

func (m *audioMixer) createUserStream(userID discord.UserID) *userStream {
	return &userStream{
		userID:       userID,
		lastUpdate:   time.Now(),
		jitterBuffer: newJitterBuffer(),
	}
}

// newJitterBuffer creates a new jitter buffer with adaptive delay
func newJitterBuffer() *jitterBuffer {
	return &jitterBuffer{
		packets: make(map[uint16]*audioPacketData),
	}
}

func (j *jitterBuffer) insert(packet *audioPacketData) {
	j.mu.Lock()
	defer j.mu.Unlock()
	
	j.packets[packet.sequence] = packet
	
	// Keep only recent packets to prevent unbounded growth
	// Remove packets older than 10 seconds
	cutoffTime := time.Now().Add(-10 * time.Second)
	for seq, p := range j.packets {
		if p.timestamp.Before(cutoffTime) {
			delete(j.packets, seq)
		}
	}
	
	// Also limit by count as a safety measure
	// 500 packets = ~10 seconds of audio (20ms per packet)
	const maxPackets = 500
	if len(j.packets) > maxPackets {
		// Remove oldest packets based on timestamp
		packets := make([]*audioPacketData, 0, len(j.packets))
		for _, p := range j.packets {
			packets = append(packets, p)
		}
		
		// Sort by timestamp (oldest first)
		for i := 0; i < len(packets)-1; i++ {
			for j := i + 1; j < len(packets); j++ {
				if packets[i].timestamp.After(packets[j].timestamp) {
					packets[i], packets[j] = packets[j], packets[i]
				}
			}
		}
		
		// Remove oldest packets
		toRemove := len(packets) - maxPackets
		for i := 0; i < toRemove; i++ {
			delete(j.packets, packets[i].sequence)
		}
	}
}



func (s *streamSynchronizer) calculateOffset(userID discord.UserID, rtpTimestamp uint32, arrivalTime time.Time) {
	// For Discord, RTP timestamps increment by 960 per 20ms frame at 48kHz
	// We're working at 24kHz, so 480 samples per 20ms
	elapsedSamples := int64(rtpTimestamp) / 2 // Convert 48kHz to 24kHz
	elapsedTime := time.Duration(elapsedSamples * 1e9 / int64(s.sampleRate))
	
	actualElapsed := arrivalTime.Sub(s.baseTime)
	offset := int64(actualElapsed - elapsedTime)
	
	// Store or update offset with smoothing
	if existingInt, ok := s.streamOffsets.Load(userID); ok {
		existing := existingInt.(int64)
		// Smooth offset changes to avoid artifacts
		s.streamOffsets.Store(userID, (existing*7+offset)/8)
	} else {
		s.streamOffsets.Store(userID, offset)
	}
}

func (m *audioMixer) GetMixedAudio(duration time.Duration) ([]byte, error) {
	startTime := time.Now()
	defer func() {
		processingTime := time.Since(startTime)
		m.avgMixDuration = (m.avgMixDuration + processingTime) / 2

		// Check if we should enter fallback mode
		if processingTime > 8*time.Millisecond {
			m.fallbackMode = true
			m.logger.Warn("Audio mixing taking too long, enabling fallback mode",
				zap.Duration("processing_time", processingTime))
		} else if m.fallbackMode && processingTime < 5*time.Millisecond {
			// Exit fallback mode if performance improves
			m.fallbackMode = false
			m.logger.Info("Audio mixing performance improved, disabling fallback mode")
		}
	}()

	// Calculate expected samples
	expectedSamples := int(duration.Nanoseconds() * int64(m.sampleRate) / int64(time.Second))
	expectedBytes := expectedSamples * 2 // 16-bit samples

	// If in fallback mode, use dominant speaker only
	if m.fallbackMode {
		return m.getDominantSpeakerAudio(duration)
	}

	// Find active users and their RTP ranges
	activeStreams := m.getActiveStreams(duration)
	if len(activeStreams) == 0 {
		// Return silence
		m.logger.Debug("No active users found, returning silence",
			zap.Duration("duration", duration),
			zap.Int("sample_count", expectedSamples))
		return make([]byte, expectedBytes), nil
	}

	if len(activeStreams) == 1 {
		// Single user, no mixing needed
		userID := activeStreams[0].userID
		m.logger.Debug("Single active user found",
			zap.String("user_id", userID.String()),
			zap.Duration("duration", duration))
		return m.getUserAudioAligned(activeStreams[0], duration), nil
	}

	// Multiple users, perform RMS-based mixing
	m.logger.Debug("Multiple active users found",
		zap.Int("count", len(activeStreams)),
		zap.Duration("duration", duration))
	return m.mixWithRMS(activeStreams, duration)
}

func (m *audioMixer) getActiveStreams(duration time.Duration) []*userStream {
	now := time.Now()
	cutoffTime := now.Add(-duration)
	var activeStreams []*userStream

	m.userStreams.Range(func(key, value any) bool {
		stream := value.(*userStream)
		if stream.lastUpdate.After(cutoffTime) {
			activeStreams = append(activeStreams, stream)
			m.logger.Debug("Found active user",
				zap.String("user_id", stream.userID.String()),
				zap.Time("last_update", stream.lastUpdate),
				zap.Duration("age", now.Sub(stream.lastUpdate)))
		}
		return true
	})
	
	m.logger.Debug("Active streams for mixing",
		zap.Int("count", len(activeStreams)),
		zap.Duration("window", duration),
		zap.Time("cutoff", cutoffTime))

	return activeStreams
}

// getUserAudioAligned returns user audio properly aligned for the given duration
func (m *audioMixer) getUserAudioAligned(stream *userStream, duration time.Duration) []byte {
	expectedSamples := int(duration.Nanoseconds() * int64(m.sampleRate) / int64(time.Second))
	expectedBytes := expectedSamples * 2 // 16-bit samples
	
	// Simple approach: get all packets from jitter buffer and concatenate them
	// then trim/pad to the expected size
	stream.jitterBuffer.mu.Lock()
	packets := make([]*audioPacketData, 0, len(stream.jitterBuffer.packets))
	for _, packet := range stream.jitterBuffer.packets {
		packets = append(packets, packet)
	}
	stream.jitterBuffer.mu.Unlock()
	
	m.logger.Debug("Retrieved ALL packets from jitter buffer",
		zap.String("user_id", stream.userID.String()),
		zap.Int("total_packets", len(packets)),
		zap.Duration("requested_duration", duration),
		zap.Int("expected_bytes", expectedBytes))
	
	if len(packets) == 0 {
		m.logger.Debug("No packets in buffer, returning silence",
			zap.String("user_id", stream.userID.String()))
		return make([]byte, expectedBytes)
	}
	
	// Sort packets by RTP timestamp
	for i := range len(packets) - 1 {
		for j := i + 1; j < len(packets); j++ {
			if packets[i].rtpTimestamp > packets[j].rtpTimestamp {
				packets[i], packets[j] = packets[j], packets[i]
			}
		}
	}
	
	// Concatenate all audio data
	result := make([]byte, 0, expectedBytes)
	totalAudioBytes := 0
	packetsUsed := 0
	
	for _, packet := range packets {
		if len(packet.audio) > 0 {
			result = append(result, packet.audio...)
			totalAudioBytes += len(packet.audio)
			packetsUsed++
			
			m.logger.Debug("Added packet audio",
				zap.String("user_id", stream.userID.String()),
				zap.Uint32("rtp_timestamp", packet.rtpTimestamp),
				zap.Int("audio_bytes", len(packet.audio)),
				zap.Int("total_so_far", len(result)))
		}
	}
	
	m.logger.Debug("Audio concatenation complete",
		zap.String("user_id", stream.userID.String()),
		zap.Int("packets_used", packetsUsed),
		zap.Int("total_audio_bytes", totalAudioBytes),
		zap.Int("result_bytes", len(result)))
	
	// Adjust to expected size
	if len(result) < expectedBytes {
		// Pad with silence at the end
		padding := make([]byte, expectedBytes-len(result))
		result = append(result, padding...)
		m.logger.Debug("Padded with silence",
			zap.String("user_id", stream.userID.String()),
			zap.Int("padding_bytes", len(padding)))
	} else if len(result) > expectedBytes {
		// Keep the most recent audio (end of the buffer)
		result = result[len(result)-expectedBytes:]
		m.logger.Debug("Trimmed to expected size",
			zap.String("user_id", stream.userID.String()),
			zap.Int("trimmed_bytes", len(result)-expectedBytes))
	}
	
	return result
}

// mixWithRMS performs RMS-weighted mixing to prevent clipping
func (m *audioMixer) mixWithRMS(streams []*userStream, duration time.Duration) ([]byte, error) {
	expectedSamples := int(duration.Nanoseconds() * int64(m.sampleRate) / int64(time.Second))
	
	// Collect aligned audio from all streams
	streamAudio := make(map[discord.UserID][]float32)
	rmsValues := make(map[discord.UserID]float32)
	totalRMS := float32(0.0)
	
	for _, stream := range streams {
		// Get aligned audio
		audioBytes := m.getUserAudioAligned(stream, duration)
		
		// Convert to float32 for mixing
		samples := m.bytesToFloat32(audioBytes)
		streamAudio[stream.userID] = samples
		
		// Calculate RMS for this stream
		rms := m.calculateRMSFloat32(samples)
		rmsValues[stream.userID] = rms
		totalRMS += rms
	}
	
	// Mix with RMS-based weights
	mixed := make([]float32, expectedSamples)
	
	if totalRMS > 0 {
		for userID, samples := range streamAudio {
			weight := rmsValues[userID] / totalRMS
			
			for i := 0; i < len(samples) && i < len(mixed); i++ {
				mixed[i] += samples[i] * weight
			}
		}
	} else {
		// If no energy, just average
		weight := 1.0 / float32(len(streams))
		for _, samples := range streamAudio {
			for i := 0; i < len(samples) && i < len(mixed); i++ {
				mixed[i] += samples[i] * weight
			}
		}
	}
	
	// Apply soft limiting to prevent clipping
	for i := range mixed {
		mixed[i] = m.softLimit(mixed[i])
	}
	
	// Convert back to bytes
	result := m.float32ToBytes(mixed)
	
	m.logger.Debug("Mixed audio with RMS weighting",
		zap.Int("num_streams", len(streams)),
		zap.Float32("total_rms", totalRMS),
		zap.Int("output_size", len(result)))
	
	return result, nil
}

// GetAllAvailableMixedAudio returns all available audio based on actual RTP timestamps
func (m *audioMixer) GetAllAvailableMixedAudio() ([]byte, time.Duration, error) {
	// Find the time range of actual audio packets we have
	var earliestTime, latestTime time.Time
	hasAudio := false
	activeStreams := make([]*userStream, 0)
	
	m.userStreams.Range(func(key, value any) bool {
		stream := value.(*userStream)
		
		stream.jitterBuffer.mu.Lock()
		if len(stream.jitterBuffer.packets) == 0 {
			stream.jitterBuffer.mu.Unlock()
			return true
		}
		
		// Find earliest and latest packet times for this stream
		for _, packet := range stream.jitterBuffer.packets {
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
		stream.jitterBuffer.mu.Unlock()
		
		activeStreams = append(activeStreams, stream)
		return true
	})
	
	if !hasAudio || len(activeStreams) == 0 {
		return []byte{}, 0, nil
	}
	
	// Calculate actual duration from packet timestamps
	actualDuration := latestTime.Sub(earliestTime)
	
	// Add one packet duration (20ms) to include the last packet
	actualDuration += 20 * time.Millisecond
	
	// Sanity check - cap at reasonable limits
	const minDuration = 100 * time.Millisecond
	const maxDuration = 30 * time.Second
	
	if actualDuration < minDuration {
		actualDuration = minDuration
	} else if actualDuration > maxDuration {
		m.logger.Warn("Capping audio duration to reasonable limit",
			zap.Duration("calculated", actualDuration),
			zap.Duration("capped", maxDuration))
		actualDuration = maxDuration
	}
	
	m.logger.Debug("Determined audio duration from packet timestamps",
		zap.Time("earliest", earliestTime),
		zap.Time("latest", latestTime),
		zap.Duration("duration", actualDuration),
		zap.Int("active_streams", len(activeStreams)))
	
	// Get mixed audio for this exact duration
	mixedAudio, err := m.GetMixedAudio(actualDuration)
	if err != nil {
		return nil, 0, err
	}
	
	m.logger.Debug("Mixed available audio",
		zap.Duration("actual_duration", actualDuration),
		zap.Int("output_size", len(mixedAudio)),
		zap.Int("active_users", len(activeStreams)))
	
	return mixedAudio, actualDuration, nil
}

// GetAllAvailableMixedAudioAndFlush gets all available mixed audio and immediately flushes buffers
func (m *audioMixer) GetAllAvailableMixedAudioAndFlush() ([]byte, time.Duration, error) {
	// Get all available audio
	mixedAudio, duration, err := m.GetAllAvailableMixedAudio()
	if err != nil {
		return nil, 0, err
	}
	
	// Immediately flush all buffers
	m.ClearAllBuffers()
	
	m.logger.Debug("Got mixed audio and flushed all buffers",
		zap.Duration("duration", duration),
		zap.Int("audio_size", len(mixedAudio)))
	
	return mixedAudio, duration, nil
}




// bytesToFloat32 converts 16-bit PCM to normalized float32
func (m *audioMixer) bytesToFloat32(audio []byte) []float32 {
	samples := make([]float32, len(audio)/2)
	for i := 0; i+1 < len(audio); i += 2 {
		sample := int16(uint16(audio[i]) | (uint16(audio[i+1]) << 8))
		samples[i/2] = float32(sample) / 32768.0
	}
	return samples
}

// float32ToBytes converts normalized float32 to 16-bit PCM
func (m *audioMixer) float32ToBytes(samples []float32) []byte {
	result := make([]byte, len(samples)*2)
	for i, sample := range samples {
		// Scale and clamp
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

// softLimit applies soft limiting to prevent harsh clipping
func (m *audioMixer) softLimit(sample float32) float32 {
	// Soft knee compression
	threshold := float32(0.8)
	abs := sample
	if abs < 0 {
		abs = -abs
	}
	
	if abs <= threshold {
		return sample
	}
	
	// Apply soft compression above threshold
	ratio := (1.0 - threshold) / (abs - threshold + 1.0)
	limited := threshold + (abs-threshold)*ratio
	
	if sample < 0 {
		return -limited
	}
	return limited
}

// calculateRMSFloat32 calculates RMS from float32 samples
func (m *audioMixer) calculateRMSFloat32(samples []float32) float32 {
	if len(samples) == 0 {
		return 0
	}
	
	sum := float32(0)
	for _, s := range samples {
		sum += s * s
	}
	
	return float32(math.Sqrt(float64(sum / float32(len(samples)))))
}


func (m *audioMixer) getDominantSpeakerAudio(duration time.Duration) ([]byte, error) {
	dominantUser, _ := m.GetDominantSpeaker()
	if dominantUser == 0 {
		// Return silence
		sampleCount := int(duration.Nanoseconds() * int64(m.sampleRate) / int64(time.Second))
		return make([]byte, sampleCount*2), nil
	}
	
	// Find the stream
	var dominantStream *userStream
	m.userStreams.Range(func(key, value any) bool {
		if key.(discord.UserID) == dominantUser {
			dominantStream = value.(*userStream)
			return false
		}
		return true
	})
	
	if dominantStream == nil {
		sampleCount := int(duration.Nanoseconds() * int64(m.sampleRate) / int64(time.Second))
		return make([]byte, sampleCount*2), nil
	}

	return m.getUserAudioAligned(dominantStream, duration), nil
}

func (m *audioMixer) ClearUserBuffer(userID discord.UserID) {
	m.userStreams.Delete(userID)
	m.synchronizer.streamOffsets.Delete(userID)

	m.logger.Debug("Cleared user buffer",
		zap.String("user_id", userID.String()))
}

func (m *audioMixer) GetDominantSpeaker() (discord.UserID, float32) {
	var dominantUser discord.UserID
	var maxEnergy float32 = 0.0
	var totalEnergy float32 = 0.0

	recentTime := time.Now().Add(-2 * time.Second) // Consider last 2 seconds

	m.userStreams.Range(func(key, value any) bool {
		stream := value.(*userStream)
		if stream.lastUpdate.After(recentTime) {
			energy := stream.energyLevel
			totalEnergy += energy

			if energy > maxEnergy {
				maxEnergy = energy
				dominantUser = stream.userID
			}
		}
		return true
	})

	// Calculate confidence as percentage of total energy
	confidence := float32(0.0)
	if totalEnergy > 0 {
		confidence = maxEnergy / totalEnergy
	}

	return dominantUser, confidence
}



// calculateRMS calculates RMS from PCM audio bytes
func (m *audioMixer) calculateRMS(audio []byte) float32 {
	if len(audio) < 2 {
		return 0.0
	}

	var sum float64
	sampleCount := len(audio) / 2

	for i := 0; i < len(audio)-1; i += 2 {
		// Convert little-endian 16-bit samples to float
		sample := int16(uint16(audio[i]) | (uint16(audio[i+1]) << 8))
		sampleFloat := float64(sample) / 32768.0 // Normalize to [-1, 1]
		sum += sampleFloat * sampleFloat
	}

	if sampleCount == 0 {
		return 0.0
	}

	rms := math.Sqrt(sum / float64(sampleCount))
	return float32(rms)
}

// ClearAllBuffers clears all user buffers
func (m *audioMixer) ClearAllBuffers() {
	m.userStreams.Range(func(key, value any) bool {
		userID := key.(discord.UserID)
		m.ClearUserBuffer(userID)
		return true
	})
	m.logger.Debug("Cleared all mixer buffers")
}


// GetUserAudioForDebug returns user audio for debugging purposes
func (m *audioMixer) GetUserAudioForDebug(userID discord.UserID, duration time.Duration) []byte {
	streamInt, exists := m.userStreams.Load(userID)
	if !exists {
		return nil
	}
	stream := streamInt.(*userStream)
	return m.getUserAudioAligned(stream, duration)
}

// GetActiveUsersForDebug returns all active users for debugging
func (m *audioMixer) GetActiveUsersForDebug() []discord.UserID {
	var users []discord.UserID
	m.userStreams.Range(func(key, value any) bool {
		users = append(users, key.(discord.UserID))
		return true
	})
	return users
}

