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

	// Get user audio for debugging
	GetUserAudioForDebug(userID discord.UserID, duration time.Duration) []byte
}

// userStream represents a single user's audio processing pipeline
type userStream struct {
	mu               sync.Mutex // Protect concurrent access to fields
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
	logger      *zap.Logger
	cfg         *config.VoiceConfig
	userStreams sync.Map // map[discord.UserID]*userStream
	sampleRate  int
	frameSize   int

	// Synchronization
	synchronizer *streamSynchronizer

	// Performance tracking (protected by mutex)
	mu             sync.Mutex
	avgMixDuration time.Duration
	fallbackMode   bool
}

// streamSynchronizer manages cross-stream synchronization
type streamSynchronizer struct {
	baseTime      time.Time
	lastRTP       sync.Map // map[discord.UserID]uint64 - unwrapped RTP timestamps
	streamOffsets sync.Map // map[discord.UserID]int64 - smoothed sample offsets
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

	// Lock stream for metadata updates
	stream.mu.Lock()

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

	// Calculate energy level (RMS)
	energyLevel := m.calculateRMS(audio)
	stream.energyLevel = energyLevel
	stream.lastRTPTimestamp = rtpTimestamp
	stream.lastSequence = sequence

	stream.mu.Unlock()

	// Update synchronization offset
	m.synchronizer.calculateOffset(userID, rtpTimestamp, timestamp)

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

		// Sort by timestamp (oldest first) using efficient sort
		sort.Slice(packets, func(i, j int) bool {
			return packets[i].timestamp.Before(packets[j].timestamp)
		})

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

	// Handle RTP timestamp wrap-around (32-bit overflow)
	var unwrappedRTP uint64
	if existingRTPInt, ok := s.lastRTP.Load(userID); ok {
		existingRTP := existingRTPInt.(uint64)
		currentRTP32 := uint32(existingRTP & 0xFFFFFFFF) // Get the 32-bit part for comparison

		// Check for wrap-around: if new timestamp is much smaller than previous, assume wrap
		if rtpTimestamp < currentRTP32 && (currentRTP32-rtpTimestamp) > (1<<31) {
			// Wrap detected: add 2^32 to the high part
			unwrappedRTP = (existingRTP & 0xFFFFFFFF00000000) + (1 << 32) + uint64(rtpTimestamp)
		} else {
			// No wrap: just update the low 32 bits
			unwrappedRTP = (existingRTP & 0xFFFFFFFF00000000) + uint64(rtpTimestamp)
		}
	} else {
		// First packet for this user
		unwrappedRTP = uint64(rtpTimestamp)
	}

	// Store the unwrapped RTP timestamp
	s.lastRTP.Store(userID, unwrappedRTP)

	elapsedSamples := int64(unwrappedRTP) / 2 // Convert 48kHz to 24kHz
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

func (s *streamSynchronizer) getOffsetForUser(userID discord.UserID) int64 {
	if offsetInt, ok := s.streamOffsets.Load(userID); ok {
		return offsetInt.(int64)
	}
	return 0
}

func (m *audioMixer) GetMixedAudio(duration time.Duration) ([]byte, error) {
	startTime := time.Now()
	defer func() {
		processingTime := time.Since(startTime)

		m.mu.Lock()
		m.avgMixDuration = (m.avgMixDuration + processingTime) / 2

		// TEMPORARILY DISABLED: Check if we should enter fallback mode
		// Fallback mode causes second user to disappear by only using dominant speaker
		if false && processingTime > 8*time.Millisecond {
			if !m.fallbackMode {
				m.fallbackMode = true
				m.logger.Warn("Audio mixing taking too long, enabling fallback mode",
					zap.Duration("processing_time", processingTime))
			}
		} else if m.fallbackMode && processingTime < 5*time.Millisecond {
			// Exit fallback mode if performance improves
			m.fallbackMode = false
			m.logger.Info("Audio mixing performance improved, disabling fallback mode")
		}
		m.mu.Unlock()
	}()

	// Calculate expected samples
	expectedSamples := int(duration.Nanoseconds() * int64(m.sampleRate) / int64(time.Second))
	expectedBytes := expectedSamples * 2 // 16-bit samples

	// Check if in fallback mode
	m.mu.Lock()
	fallback := m.fallbackMode
	m.mu.Unlock()

	// If in fallback mode, use dominant speaker only
	if fallback {
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

	// Calculate global earliest RTP timestamp across all streams
	// This is critical for proper multi-user synchronization
	var globalEarliestRTP uint32 = math.MaxUint32
	streamRTPRanges := make(map[discord.UserID]struct{ earliest, latest uint32 })

	for _, stream := range activeStreams {
		stream.jitterBuffer.mu.Lock()
		var streamEarliest, streamLatest uint32 = math.MaxUint32, 0
		packetCount := 0
		
		for _, pkt := range stream.jitterBuffer.packets {
			if pkt.rtpTimestamp < streamEarliest {
				streamEarliest = pkt.rtpTimestamp
			}
			if pkt.rtpTimestamp > streamLatest {
				streamLatest = pkt.rtpTimestamp
			}
			if pkt.rtpTimestamp < globalEarliestRTP {
				globalEarliestRTP = pkt.rtpTimestamp
			}
			packetCount++
		}
		stream.jitterBuffer.mu.Unlock()

		if packetCount > 0 {
			streamRTPRanges[stream.userID] = struct{ earliest, latest uint32 }{streamEarliest, streamLatest}
			m.logger.Debug("Stream RTP range",
				zap.String("user_id", stream.userID.String()),
				zap.Uint32("earliest_rtp", streamEarliest),
				zap.Uint32("latest_rtp", streamLatest),
				zap.Int("packets", packetCount))
		}
	}

	m.logger.Debug("Calculated global earliest RTP for synchronization",
		zap.Uint32("global_earliest_rtp", globalEarliestRTP),
		zap.Int("active_streams", len(activeStreams)),
		zap.Int("streams_with_data", len(streamRTPRanges)))

	if len(activeStreams) == 1 {
		// Single user, no mixing needed
		userID := activeStreams[0].userID
		m.logger.Debug("Single active user found",
			zap.String("user_id", userID.String()),
			zap.Duration("duration", duration))
		return m.getUserAudioAligned(activeStreams[0], duration, globalEarliestRTP), nil
	}

	// Multiple users, perform RMS-based mixing
	m.logger.Info("Mixing multiple active users - CRITICAL PATH",
		zap.Int("count", len(activeStreams)),
		zap.Duration("duration", duration),
		zap.Uint32("global_earliest_rtp", globalEarliestRTP))
	
	// Log each user's timing info for multi-user debugging
	for userID, rtpRange := range streamRTPRanges {
		deltaFromGlobal := rtpRange.earliest - globalEarliestRTP
		paddingSamples := int(deltaFromGlobal / 2)
		m.logger.Info("User timing for mixing",
			zap.String("user_id", userID.String()),
			zap.Uint32("earliest_rtp", rtpRange.earliest),
			zap.Uint32("latest_rtp", rtpRange.latest),
			zap.Uint32("delta_from_global", deltaFromGlobal),
			zap.Int("will_pad_samples", paddingSamples))
	}
	
	return m.mixWithRMS(activeStreams, duration, globalEarliestRTP)
}

func (m *audioMixer) getActiveStreams(duration time.Duration) []*userStream {
	now := time.Now()
	// CRITICAL FIX: Use actual duration instead of fixed window
	// This ensures users who spoke within the mixing window are included
	cutoffTime := now.Add(-duration)
	var activeStreams []*userStream

	m.userStreams.Range(func(key, value any) bool {
		stream := value.(*userStream)
		stream.mu.Lock()
		if stream.lastUpdate.After(cutoffTime) {
			activeStreams = append(activeStreams, stream)
			m.logger.Debug("Found active user",
				zap.String("user_id", stream.userID.String()),
				zap.Time("last_update", stream.lastUpdate),
				zap.Duration("age", now.Sub(stream.lastUpdate)))
		}
		stream.mu.Unlock()
		return true
	})

	m.logger.Debug("Active streams for mixing",
		zap.Int("count", len(activeStreams)),
		zap.Duration("requested_duration", duration),
		zap.Time("cutoff", cutoffTime))

	return activeStreams
}

// getUserAudioAligned returns user audio properly aligned for the given duration
func (m *audioMixer) getUserAudioAligned(stream *userStream, duration time.Duration, globalZeroRTP uint32) []byte {
	expectedSamples := int(duration.Nanoseconds() * int64(m.sampleRate) / int64(time.Second))
	expectedBytes := expectedSamples * 2 // 16-bit samples

	// Get all packets from jitter buffer
	stream.jitterBuffer.mu.Lock()
	packets := make([]*audioPacketData, 0, len(stream.jitterBuffer.packets))
	for _, packet := range stream.jitterBuffer.packets {
		packets = append(packets, packet)
	}
	stream.jitterBuffer.mu.Unlock()

	m.logger.Debug("Retrieved packets from jitter buffer",
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
	sort.Slice(packets, func(i, j int) bool {
		return packets[i].rtpTimestamp < packets[j].rtpTimestamp
	})

	// CRITICAL FIX: Add front padding for late-starting speakers
	// Find the earliest RTP timestamp for this stream
	streamStartRTP := packets[0].rtpTimestamp

	// Calculate padding based on the global earliest RTP reference
	// Handle potential integer overflow by using signed arithmetic
	var padSamples int
	if streamStartRTP >= globalZeroRTP {
		deltaRTP := streamStartRTP - globalZeroRTP
		padSamples = int(deltaRTP / 2) // Convert RTP samples to 24kHz samples (48k → 24k = divide by 2)
	} else {
		// Handle RTP timestamp wrap-around or earlier starting streams
		// In this case, this stream started before the "global zero", so no padding needed
		padSamples = 0
		m.logger.Debug("Stream started before global zero RTP, no padding needed",
			zap.String("user_id", stream.userID.String()),
			zap.Uint32("stream_start_rtp", streamStartRTP),
			zap.Uint32("global_zero_rtp", globalZeroRTP))
	}
	// Cap at expectedSamples to prevent excessive padding
	if padSamples > expectedSamples {
		padSamples = expectedSamples
	}
	silenceBytes := padSamples * 2

	m.logger.Debug("Calculating front padding for late starter",
		zap.String("user_id", stream.userID.String()),
		zap.Uint32("stream_start_rtp", streamStartRTP),
		zap.Uint32("global_zero_rtp", globalZeroRTP),
		zap.Int("pad_samples", padSamples),
		zap.Int("silence_bytes", silenceBytes))

	// Build result starting with front padding
	result := make([]byte, 0, expectedBytes)
	if silenceBytes > 0 {
		result = append(result, make([]byte, silenceBytes)...)
		m.logger.Debug("Inserted leading silence for alignment",
			zap.String("user_id", stream.userID.String()),
			zap.Int("pad_samples", padSamples))
	}

	// Track used sequences for deletion
	usedSequences := make([]uint16, 0, len(packets))
	packetsUsed := 0

	// Concatenate actual packet audio with gap detection
	for i, packet := range packets {
		if len(packet.audio) == 0 {
			continue
		}

		// Handle gaps between packets by inserting silence
		if i > 0 {
			prevPacket := packets[i-1]
			expectedDeltaSamples := int((packet.rtpTimestamp - prevPacket.rtpTimestamp) / 2) // 48k→24k conversion
			actualSamplesFromPrev := len(prevPacket.audio) / 2
			gapSamples := expectedDeltaSamples - actualSamplesFromPrev

			if gapSamples > 0 && gapSamples < 2400 { // Cap at 100ms to avoid huge gaps
				// Insert silence for positive gaps
				gapBytes := gapSamples * 2 // 16-bit samples
				silenceGap := make([]byte, gapBytes)
				result = append(result, silenceGap...)

				m.logger.Debug("Inserted silence for RTP gap",
					zap.String("user_id", stream.userID.String()),
					zap.Int("gap_samples", gapSamples),
					zap.Uint32("prev_rtp", prevPacket.rtpTimestamp),
					zap.Uint32("curr_rtp", packet.rtpTimestamp))
			} else if gapSamples < 0 {
				// Overlap scenario: drop the last |gapSamples| samples from result
				trimBytes := (-gapSamples) * 2
				if trimBytes < len(result) {
					result = result[:len(result)-trimBytes]
				} else {
					// In case the overlap is larger than what we have, just reset to no content
					result = []byte{}
				}

				m.logger.Debug("Trimmed overlapping audio",
					zap.String("user_id", stream.userID.String()),
					zap.Int("overlap_samples", -gapSamples),
					zap.Int("trimmed_bytes", trimBytes),
					zap.Uint32("prev_rtp", prevPacket.rtpTimestamp),
					zap.Uint32("curr_rtp", packet.rtpTimestamp))
			}
		}

		result = append(result, packet.audio...)
		packetsUsed++
		usedSequences = append(usedSequences, packet.sequence)

		m.logger.Debug("Added packet audio",
			zap.String("user_id", stream.userID.String()),
			zap.Uint32("rtp_timestamp", packet.rtpTimestamp),
			zap.Int("audio_bytes", len(packet.audio)),
			zap.Int("total_so_far", len(result)))
	}

	// Remove used packets from jitter buffer to prevent replaying
	stream.jitterBuffer.mu.Lock()
	for _, sequence := range usedSequences {
		delete(stream.jitterBuffer.packets, sequence)
	}
	stream.jitterBuffer.mu.Unlock()

	m.logger.Debug("Audio concatenation complete",
		zap.String("user_id", stream.userID.String()),
		zap.Int("packets_used", packetsUsed),
		zap.Int("front_padding_bytes", silenceBytes),
		zap.Int("result_bytes", len(result)))

	// Adjust to expected size
	if len(result) > expectedBytes {
		// CRITICAL FIX: Always keep the **end** of the buffer (most recent samples)
		// This ensures late-arriving users don't get their audio trimmed away
		result = result[len(result)-expectedBytes:]
		m.logger.Debug("Trimmed to keep most recent audio",
			zap.String("user_id", stream.userID.String()),
			zap.Int("trimmed_bytes", len(result)-expectedBytes))
	} else if len(result) < expectedBytes {
		// Pad with silence at the end
		padding := make([]byte, expectedBytes-len(result))
		result = append(result, padding...)
		m.logger.Debug("Padded trailing silence",
			zap.String("user_id", stream.userID.String()),
			zap.Int("padding_bytes", len(padding)))
	}

	return result
}

// mixWithRMS performs RMS-weighted mixing to prevent clipping
func (m *audioMixer) mixWithRMS(streams []*userStream, duration time.Duration, globalZeroRTP uint32) ([]byte, error) {
	expectedSamples := int(duration.Nanoseconds() * int64(m.sampleRate) / int64(time.Second))

	// Collect aligned audio from all streams
	streamAudio := make(map[discord.UserID][]float32)
	rmsValues := make(map[discord.UserID]float32)
	totalRMS := float32(0.0)

	for _, stream := range streams {
		// Get aligned audio
		audioBytes := m.getUserAudioAligned(stream, duration, globalZeroRTP)

		// Convert to float32 for mixing
		samples := m.bytesToFloat32(audioBytes)
		streamAudio[stream.userID] = samples

		// Calculate RMS for this stream
		rms := m.calculateRMSFloat32(samples)
		rmsValues[stream.userID] = rms
		totalRMS += rms
	}

	// Use RMS-aware dynamic mixing instead of simple averaging
	// This preserves audio quality better for multiple speakers
	mixed := make([]float32, expectedSamples)
	
	// Calculate adaptive weight based on RMS levels to prevent over-quieting
	// Instead of simple 1/N division, use RMS-weighted approach
	var adaptiveWeight float32 = 1.0
	if len(streams) > 1 {
		// Use sqrt scaling instead of linear to preserve loudness better
		// This prevents the "everyone sounds quiet" problem
		adaptiveWeight = float32(math.Sqrt(1.0 / float64(len(streams))))
		if adaptiveWeight < 0.5 {
			adaptiveWeight = 0.5 // Minimum weight to keep audio audible
		}
	}

	m.logger.Debug("Using RMS-aware dynamic mixing",
		zap.Int("num_streams", len(streams)),
		zap.Float32("adaptive_weight", adaptiveWeight),
		zap.Float32("total_rms", totalRMS))

	// Convert streamAudio map to slice for indexed access
	streamSamples := make([][]float32, 0, len(streamAudio))
	userIDs := make([]discord.UserID, 0, len(streamAudio))
	for userID, samples := range streamAudio {
		streamSamples = append(streamSamples, samples)
		userIDs = append(userIDs, userID)
		m.logger.Debug("Added stream to mix preparation",
			zap.String("user_id", userID.String()),
			zap.Float32("rms", rmsValues[userID]),
			zap.Int("samples", len(samples)))
	}

	// Apply improved dynamic mixing with smarter peak limiting
	for i := 0; i < expectedSamples; i++ {
		// Sum all streams at index i first
		sum := float32(0)
		activeSampleCount := 0
		for _, samples := range streamSamples {
			if i < len(samples) {
				sum += samples[i]
				activeSampleCount++
			}
		}

		// Apply adaptive weight based on actual active samples at this position
		// This handles cases where streams have different lengths
		var finalSample float32
		if activeSampleCount > 0 {
			// Calculate per-sample adaptive scaling
			absSum := sum
			if absSum < 0 {
				absSum = -absSum
			}

			// Use gentler compression curve to prevent distortion
			// Instead of hard limiting at 0.8, use a softer knee
			var compressionScale float32 = 1.0
			if absSum > 0.7 {
				// Soft knee compression starting at 0.7, reaching 0.9 limit
				compressionRatio := 0.9 / absSum
				if compressionRatio < 0.7 {
					compressionRatio = 0.7 // Don't over-compress
				}
				compressionScale = compressionRatio
			}

			finalSample = sum * adaptiveWeight * compressionScale
		} else {
			finalSample = 0
		}

		mixed[i] = finalSample
	}

	// Convert back to bytes
	result := m.float32ToBytes(mixed)

	// Calculate final audio statistics for debugging
	finalRMS := m.calculateRMSFloat32(mixed)
	peakSample := float32(0)
	for _, sample := range mixed {
		abs := sample
		if abs < 0 {
			abs = -abs
		}
		if abs > peakSample {
			peakSample = abs
		}
	}

	m.logger.Debug("Mixed audio with improved RMS-aware algorithm",
		zap.Int("num_streams", len(streams)),
		zap.Float32("input_total_rms", totalRMS),
		zap.Float32("output_rms", finalRMS),
		zap.Float32("output_peak", peakSample),
		zap.Float32("adaptive_weight", adaptiveWeight),
		zap.Int("output_size", len(result)))

	// Log per-stream contribution for debugging multi-user issues
	if len(streams) > 1 {
		for userID, rms := range rmsValues {
			contribution := rms / totalRMS * 100
			m.logger.Debug("Stream mixing contribution",
				zap.String("user_id", userID.String()),
				zap.Float32("stream_rms", rms),
				zap.Float32("contribution_percent", contribution))
		}
	}

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

	// Reset baseTime to prevent huge offsets after flush
	m.synchronizer.baseTime = time.Now()

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

	// Calculate global earliest RTP for this single stream
	var globalEarliestRTP uint32 = math.MaxUint32
	dominantStream.jitterBuffer.mu.Lock()
	for _, pkt := range dominantStream.jitterBuffer.packets {
		if pkt.rtpTimestamp < globalEarliestRTP {
			globalEarliestRTP = pkt.rtpTimestamp
		}
	}
	dominantStream.jitterBuffer.mu.Unlock()

	return m.getUserAudioAligned(dominantStream, duration, globalEarliestRTP), nil
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
		stream.mu.Lock()
		if stream.lastUpdate.After(recentTime) {
			energy := stream.energyLevel
			totalEnergy += energy

			if energy > maxEnergy {
				maxEnergy = energy
				dominantUser = stream.userID
			}
		}
		stream.mu.Unlock()
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

// ClearAllBuffers clears all user buffers but preserves user metadata
func (m *audioMixer) ClearAllBuffers() {
	m.userStreams.Range(func(key, value any) bool {
		stream := value.(*userStream)
		stream.mu.Lock()
		stream.jitterBuffer.mu.Lock()
		stream.jitterBuffer.packets = make(map[uint16]*audioPacketData)
		stream.jitterBuffer.mu.Unlock()
		stream.mu.Unlock()
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

	// Calculate global earliest RTP for this single stream
	var globalEarliestRTP uint32 = math.MaxUint32
	stream.jitterBuffer.mu.Lock()
	for _, pkt := range stream.jitterBuffer.packets {
		if pkt.rtpTimestamp < globalEarliestRTP {
			globalEarliestRTP = pkt.rtpTimestamp
		}
	}
	stream.jitterBuffer.mu.Unlock()

	return m.getUserAudioAligned(stream, duration, globalEarliestRTP)
}
