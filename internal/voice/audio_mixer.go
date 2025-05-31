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
	// Add user audio to buffer
	AddUserAudio(userID discord.UserID, audio []byte, timestamp time.Time) error

	// Get mixed audio for a time window
	GetMixedAudio(duration time.Duration) ([]byte, error)

	// Clear buffers for a user
	ClearUserBuffer(userID discord.UserID)

	// Get dominant speaker
	GetDominantSpeaker() (discord.UserID, float32) // returns userID, confidence
}

type UserAudioBuffer struct {
	UserID      discord.UserID
	AudioData   [][]byte
	Timestamps  []time.Time
	EnergyLevel float32
	LastUpdate  time.Time
}

type audioMixer struct {
	logger      *zap.Logger
	cfg         *config.VoiceConfig
	userBuffers map[discord.UserID]*UserAudioBuffer
	mu          sync.RWMutex

	// Performance tracking
	lastMixTime     time.Time
	avgMixDuration  time.Duration
	fallbackMode    bool
}

func NewAudioMixer(logger *zap.Logger, cfg *config.Config) AudioMixer {
	return &audioMixer{
		logger:      logger,
		cfg:         &cfg.Voice,
		userBuffers: make(map[discord.UserID]*UserAudioBuffer),
	}
}

func (m *audioMixer) AddUserAudio(userID discord.UserID, audio []byte, timestamp time.Time) error {
	if len(audio) == 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Get or create user buffer
	buffer, exists := m.userBuffers[userID]
	if !exists {
		buffer = &UserAudioBuffer{
			UserID:     userID,
			AudioData:  make([][]byte, 0, 10),
			Timestamps: make([]time.Time, 0, 10),
			LastUpdate: timestamp,
		}
		m.userBuffers[userID] = buffer
	}

	// Calculate energy level for this audio chunk
	energyLevel := m.calculateEnergyLevel(audio)
	buffer.EnergyLevel = energyLevel
	buffer.LastUpdate = timestamp

	// Add audio chunk to buffer
	audioCopy := make([]byte, len(audio))
	copy(audioCopy, audio)

	buffer.AudioData = append(buffer.AudioData, audioCopy)
	buffer.Timestamps = append(buffer.Timestamps, timestamp)

	// Keep only recent audio (last 5 seconds worth)
	maxAge := 5 * time.Second
	cutoffTime := timestamp.Add(-maxAge)

	// Remove old audio chunks
	newIndex := 0
	for i, ts := range buffer.Timestamps {
		if ts.After(cutoffTime) {
			buffer.AudioData[newIndex] = buffer.AudioData[i]
			buffer.Timestamps[newIndex] = buffer.Timestamps[i]
			newIndex++
		}
	}

	buffer.AudioData = buffer.AudioData[:newIndex]
	buffer.Timestamps = buffer.Timestamps[:newIndex]

	m.logger.Debug("Added user audio",
		zap.String("user_id", userID.String()),
		zap.Int("audio_size", len(audio)),
		zap.Float32("energy_level", energyLevel),
		zap.Int("buffer_chunks", len(buffer.AudioData)))

	return nil
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

	m.mu.RLock()
	defer m.mu.RUnlock()

	// If in fallback mode, use dominant speaker only
	if m.fallbackMode {
		return m.getDominantSpeakerAudio(duration)
	}

	// Get active users (those with recent audio)
	activeUsers := m.getActiveUsers(duration)
	if len(activeUsers) == 0 {
		// Return silence
		sampleCount := int(duration.Nanoseconds() * OpenAISampleRate / int64(time.Second))
		return make([]byte, sampleCount*2), nil // 16-bit samples
	}

	if len(activeUsers) == 1 {
		// Single user, no mixing needed
		return m.getUserAudio(activeUsers[0], duration), nil
	}

	// Multiple users, perform mixing
	return m.mixMultipleUsers(activeUsers, duration)
}

func (m *audioMixer) getActiveUsers(duration time.Duration) []discord.UserID {
	cutoffTime := time.Now().Add(-duration)
	var activeUsers []discord.UserID

	for userID, buffer := range m.userBuffers {
		if buffer.LastUpdate.After(cutoffTime) && len(buffer.AudioData) > 0 {
			activeUsers = append(activeUsers, userID)
		}
	}

	return activeUsers
}

func (m *audioMixer) getUserAudio(userID discord.UserID, duration time.Duration) []byte {
	buffer := m.userBuffers[userID]
	if buffer == nil || len(buffer.AudioData) == 0 {
		sampleCount := int(duration.Nanoseconds() * OpenAISampleRate / int64(time.Second))
		return make([]byte, sampleCount*2)
	}

	// Concatenate recent audio chunks
	var result []byte
	for _, chunk := range buffer.AudioData {
		result = append(result, chunk...)
	}

	// Ensure we have the right duration
	expectedSamples := int(duration.Nanoseconds() * OpenAISampleRate / int64(time.Second))
	expectedBytes := expectedSamples * 2 // 16-bit samples

	if len(result) < expectedBytes {
		// Pad with silence
		padding := make([]byte, expectedBytes-len(result))
		result = append(result, padding...)
	} else if len(result) > expectedBytes {
		// Truncate
		result = result[:expectedBytes]
	}

	return result
}

func (m *audioMixer) mixMultipleUsers(activeUsers []discord.UserID, duration time.Duration) ([]byte, error) {
	expectedSamples := int(duration.Nanoseconds() * OpenAISampleRate / int64(time.Second))
	expectedBytes := expectedSamples * 2 // 16-bit samples

	// Collect audio from all users
	userAudioData := make(map[discord.UserID][]byte)
	userWeights := make(map[discord.UserID]float32)

	totalEnergy := float32(0.0)

	for _, userID := range activeUsers {
		audio := m.getUserAudio(userID, duration)
		userAudioData[userID] = audio

		// Calculate weight based on energy level
		buffer := m.userBuffers[userID]
		energy := buffer.EnergyLevel
		userWeights[userID] = energy
		totalEnergy += energy
	}

	// Normalize weights
	if totalEnergy > 0 {
		for userID := range userWeights {
			userWeights[userID] = userWeights[userID] / totalEnergy
		}
	}

	// Mix audio using weighted sum
	mixed := make([]int32, expectedSamples) // Use int32 to prevent overflow

	for userID, audio := range userAudioData {
		weight := userWeights[userID]
		
		for i := 0; i < len(audio)-1; i += 2 {
			// Convert little-endian 16-bit to int32
			sample := int32(int16(audio[i]) | (int16(audio[i+1]) << 8))
			
			// Apply weight and add to mix
			weightedSample := int32(float32(sample) * weight)
			sampleIndex := i / 2
			
			if sampleIndex < len(mixed) {
				mixed[sampleIndex] += weightedSample
			}
		}
	}

	// Convert back to 16-bit and apply compression
	result := make([]byte, expectedBytes)
	for i, sample := range mixed {
		// Apply dynamic range compression
		compressed := m.applyCompression(sample)
		
		// Clamp to 16-bit range
		if compressed > 32767 {
			compressed = 32767
		} else if compressed < -32768 {
			compressed = -32768
		}

		// Convert back to little-endian bytes
		sample16 := int16(compressed)
		byteIndex := i * 2
		if byteIndex < len(result)-1 {
			result[byteIndex] = byte(sample16)
			result[byteIndex+1] = byte(sample16 >> 8)
		}
	}

	return result, nil
}

func (m *audioMixer) applyCompression(sample int32) int32 {
	// Simple dynamic range compression
	// Reduces loud sounds and amplifies quiet sounds
	
	absValue := sample
	if absValue < 0 {
		absValue = -absValue
	}

	// Apply compression curve
	compressedAbs := int32(float64(absValue) * (1.0 - float64(absValue)/65536.0))
	
	if sample < 0 {
		return -compressedAbs
	}
	return compressedAbs
}

func (m *audioMixer) getDominantSpeakerAudio(duration time.Duration) ([]byte, error) {
	dominantUser, _ := m.GetDominantSpeaker()
	if dominantUser == 0 {
		// Return silence
		sampleCount := int(duration.Nanoseconds() * OpenAISampleRate / int64(time.Second))
		return make([]byte, sampleCount*2), nil
	}

	return m.getUserAudio(dominantUser, duration), nil
}

func (m *audioMixer) ClearUserBuffer(userID discord.UserID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.userBuffers, userID)

	m.logger.Debug("Cleared user buffer",
		zap.String("user_id", userID.String()))
}

func (m *audioMixer) GetDominantSpeaker() (discord.UserID, float32) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var dominantUser discord.UserID
	var maxEnergy float32 = 0.0
	var totalEnergy float32 = 0.0

	recentTime := time.Now().Add(-2 * time.Second) // Consider last 2 seconds

	for userID, buffer := range m.userBuffers {
		if buffer.LastUpdate.After(recentTime) {
			energy := buffer.EnergyLevel
			totalEnergy += energy

			if energy > maxEnergy {
				maxEnergy = energy
				dominantUser = userID
			}
		}
	}

	// Calculate confidence as percentage of total energy
	confidence := float32(0.0)
	if totalEnergy > 0 {
		confidence = maxEnergy / totalEnergy
	}

	return dominantUser, confidence
}

func (m *audioMixer) calculateEnergyLevel(audio []byte) float32 {
	if len(audio) < 2 {
		return 0.0
	}

	var sum float64
	sampleCount := len(audio) / 2

	for i := 0; i < len(audio)-1; i += 2 {
		// Convert little-endian 16-bit samples to float
		sample := int16(audio[i]) | (int16(audio[i+1]) << 8)
		sampleFloat := float64(sample) / 32768.0 // Normalize to [-1, 1]
		sum += sampleFloat * sampleFloat
	}

	if sampleCount == 0 {
		return 0.0
	}

	rms := math.Sqrt(sum / float64(sampleCount))
	return float32(rms)
}

// GetMixerStats returns performance statistics
func (m *audioMixer) GetMixerStats() MixerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := MixerStats{
		ActiveUsers:     len(m.userBuffers),
		FallbackMode:    m.fallbackMode,
		AvgMixDuration:  m.avgMixDuration,
		LastMixTime:     m.lastMixTime,
	}

	return stats
}

type MixerStats struct {
	ActiveUsers     int
	FallbackMode    bool
	AvgMixDuration  time.Duration
	LastMixTime     time.Time
}