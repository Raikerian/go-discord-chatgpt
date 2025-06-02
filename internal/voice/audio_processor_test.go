package voice_test

import (
	"encoding/base64"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/Raikerian/go-discord-chatgpt/internal/voice"
)

// Test constants based on the audio processor requirements
const (
	testSampleRate24k = 24000
	testSampleRate48k = 48000
	testChannelsMono  = 1
	testChannelsStereo = 2
	testFrameSize20ms = 480  // 20ms at 24kHz
	testFrameSizeDiscord = 960 // 20ms at 48kHz
)

func TestAudioProcessor_OpusToPCM(t *testing.T) {
	processor := createTestProcessor(t)
	defer processor.Close()

	tests := map[string]struct {
		input       []byte
		expectError bool
		description string
	}{
		"empty_opus_data": {
			input:       []byte{},
			expectError: true,
			description: "Should reject empty opus data",
		},
		"nil_opus_data": {
			input:       nil,
			expectError: true,
			description: "Should reject nil opus data",
		},
		"valid_opus_frame": {
			input:       generateMockOpusFrame(),
			expectError: false,
			description: "Should process valid opus frame",
		},
		"corrupted_opus_data": {
			input:       []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			expectError: true,
			description: "Should reject corrupted opus data",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pcm, err := processor.OpusToPCM(tt.input)

			if tt.expectError {
				assert.Error(t, err, tt.description)
				assert.Nil(t, pcm)
			} else {
				assert.NoError(t, err, tt.description)
				assert.NotNil(t, pcm)
				assert.Greater(t, len(pcm), 0, "PCM output should not be empty")
				
				// Validate PCM output format for OpenAI (24kHz mono, 16-bit)
				// Note: actual frame size depends on input Opus frame duration
				assert.Equal(t, 0, len(pcm)%2, "PCM should have even byte length for 16-bit samples")
			}
		})
	}
}

func TestAudioProcessor_PCMToBase64(t *testing.T) {
	processor := createTestProcessor(t)
	defer processor.Close()

	tests := map[string]struct {
		input       []byte
		expectError bool
		description string
	}{
		"empty_pcm_data": {
			input:       []byte{},
			expectError: true,
			description: "Should reject empty PCM data",
		},
		"nil_pcm_data": {
			input:       nil,
			expectError: true,
			description: "Should reject nil PCM data",
		},
		"valid_pcm_16bit": {
			input:       generateTestPCM16(testFrameSize20ms),
			expectError: false,
			description: "Should encode valid 16-bit PCM data",
		},
		"large_pcm_chunk": {
			input:       generateTestPCM16(testSampleRate24k), // 1 second
			expectError: false,
			description: "Should handle large PCM chunks",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			encoded, err := processor.PCMToBase64(tt.input)

			if tt.expectError {
				assert.Error(t, err, tt.description)
				assert.Empty(t, encoded)
			} else {
				assert.NoError(t, err, tt.description)
				assert.NotEmpty(t, encoded)
				
				// Verify base64 encoding validity
				decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
				assert.NoError(t, decodeErr, "Should produce valid base64")
				assert.Equal(t, tt.input, decoded, "Round-trip should preserve data")
				
				// Verify encoded length
				expectedLen := base64.StdEncoding.EncodedLen(len(tt.input))
				assert.Equal(t, expectedLen, len(encoded), "Base64 length should be correct")
			}
		})
	}
}

func TestAudioProcessor_Base64ToPCM(t *testing.T) {
	processor := createTestProcessor(t)
	defer processor.Close()

	validPCM := generateTestPCM16(testFrameSize20ms)
	validBase64 := base64.StdEncoding.EncodeToString(validPCM)

	tests := map[string]struct {
		input       string
		expectError bool
		description string
	}{
		"empty_base64": {
			input:       "",
			expectError: true,
			description: "Should reject empty base64 string",
		},
		"invalid_base64": {
			input:       "not-valid-base64!@#$",
			expectError: true,
			description: "Should reject invalid base64",
		},
		"valid_base64_pcm": {
			input:       validBase64,
			expectError: false,
			description: "Should decode valid base64 PCM",
		},
		"odd_length_pcm": {
			input:       base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03}), // 3 bytes (not multiple of 2)
			expectError: true,
			description: "Should reject PCM with odd byte length",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pcm, err := processor.Base64ToPCM(tt.input)

			if tt.expectError {
				assert.Error(t, err, tt.description)
				assert.Nil(t, pcm)
			} else {
				assert.NoError(t, err, tt.description)
				assert.NotNil(t, pcm)
				
				// Verify PCM format (16-bit, so even byte length)
				assert.Equal(t, 0, len(pcm)%2, "PCM should have even byte length for 16-bit samples")
				
				// Round-trip test
				reencoded, err := processor.PCMToBase64(pcm)
				assert.NoError(t, err)
				assert.Equal(t, tt.input, reencoded, "Round-trip should preserve data")
			}
		})
	}
}

func TestAudioProcessor_PCMToOpus(t *testing.T) {
	processor := createTestProcessor(t)
	defer processor.Close()

	tests := map[string]struct {
		input       []byte
		expectError bool
		description string
	}{
		"empty_pcm_data": {
			input:       []byte{},
			expectError: true,
			description: "Should reject empty PCM data",
		},
		"nil_pcm_data": {
			input:       nil,
			expectError: true,
			description: "Should reject nil PCM data",
		},
		"valid_pcm_20ms": {
			input:       generateTestPCM16(testFrameSize20ms),
			expectError: false,
			description: "Should encode 20ms PCM frame",
		},
		"oversized_pcm": {
			input:       generateTestPCM16(testFrameSize20ms * 3), // 60ms
			expectError: false,
			description: "Should handle oversized PCM by truncating",
		},
		"undersized_pcm": {
			input:       generateTestPCM16(testFrameSize20ms / 2), // 10ms
			expectError: false,
			description: "Should handle undersized PCM by padding",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			opus, err := processor.PCMToOpus(tt.input)

			if tt.expectError {
				assert.Error(t, err, tt.description)
				assert.Nil(t, opus)
			} else {
				assert.NoError(t, err, tt.description)
				assert.NotNil(t, opus)
				assert.Greater(t, len(opus), 0, "Opus output should not be empty")
				
				// Opus frames should be reasonably sized (not too small/large)
				assert.Less(t, len(opus), 4000, "Opus frame should not exceed max size")
				assert.Greater(t, len(opus), 10, "Opus frame should not be too small")
			}
		})
	}
}

func TestAudioProcessor_DetectSilence(t *testing.T) {
	processor := createTestProcessor(t)
	defer processor.Close()

	tests := map[string]struct {
		audio           []byte
		expectedSilent  bool
		description     string
		energyThreshold float32
	}{
		"digital_silence": {
			audio:           make([]byte, testFrameSize20ms*2), // All zeros
			expectedSilent:  true,
			description:     "Should detect digital silence (all zeros)",
			energyThreshold: 0.0,
		},
		"high_energy_audio": {
			audio:           generateHighEnergyAudio(testFrameSize20ms),
			expectedSilent:  false,
			description:     "Should detect high energy audio as not silent",
			energyThreshold: 0.1,
		},
		"low_background_noise": {
			audio:           generateLowNoiseAudio(testFrameSize20ms),
			expectedSilent:  true,
			description:     "Should detect low background noise as silent",
			energyThreshold: 0.005, // Adjusted to realistic low noise level
		},
		"empty_audio": {
			audio:           []byte{},
			expectedSilent:  true,
			description:     "Should treat empty audio as silent",
			energyThreshold: 0.0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			isSilent, energy := processor.DetectSilence(tt.audio)

			assert.Equal(t, tt.expectedSilent, isSilent, tt.description)
			assert.GreaterOrEqual(t, energy, float32(0.0), "Energy should be non-negative")
			
			if tt.expectedSilent {
				assert.LessOrEqual(t, energy, tt.energyThreshold, "Silent audio should have low energy")
			} else {
				assert.Greater(t, energy, tt.energyThreshold, "Non-silent audio should have higher energy")
			}
		})
	}
}

func TestAudioProcessor_ConfigureQuality(t *testing.T) {
	processor := createTestProcessor(t)
	defer processor.Close()

	tests := map[string]struct {
		quality     string
		expectError bool
		description string
	}{
		"low_quality": {
			quality:     "low",
			expectError: false,
			description: "Should accept low quality preset",
		},
		"medium_quality": {
			quality:     "medium",
			expectError: false,
			description: "Should accept medium quality preset",
		},
		"high_quality": {
			quality:     "high",
			expectError: false,
			description: "Should accept high quality preset",
		},
		"invalid_quality": {
			quality:     "ultra-mega-high",
			expectError: true,
			description: "Should reject unknown quality preset",
		},
		"empty_quality": {
			quality:     "",
			expectError: true,
			description: "Should reject empty quality string",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := processor.ConfigureQuality(tt.quality)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestAudioProcessor_Close(t *testing.T) {
	processor := createTestProcessor(t)

	// Should close successfully
	err := processor.Close()
	assert.NoError(t, err, "First close should succeed")

	// Should handle multiple closes gracefully
	err = processor.Close()
	assert.NoError(t, err, "Second close should not error")

	// Should reject operations after close
	_, err = processor.OpusToPCM(generateMockOpusFrame())
	assert.Error(t, err, "Operations should fail after close")
	assert.Contains(t, err.Error(), "closed", "Error should mention processor is closed")
}

func TestAudioProcessor_RoundTripConversion(t *testing.T) {
	processor := createTestProcessor(t)
	defer processor.Close()

	t.Run("opus_to_pcm_to_base64_roundtrip", func(t *testing.T) {
		// Start with mock Opus data
		opusData := generateMockOpusFrame()

		// Convert Opus to PCM
		pcm, err := processor.OpusToPCM(opusData)
		require.NoError(t, err)

		// Convert PCM to Base64
		base64Audio, err := processor.PCMToBase64(pcm)
		require.NoError(t, err)

		// Convert Base64 back to PCM
		pcmBack, err := processor.Base64ToPCM(base64Audio)
		require.NoError(t, err)

		// Should be identical
		assert.Equal(t, pcm, pcmBack, "Round-trip PCM->Base64->PCM should preserve data")
	})

	t.Run("pcm_to_opus_basic", func(t *testing.T) {
		// Generate test PCM (24kHz mono)
		pcm := generateTestPCM16(testFrameSize20ms)

		// Convert to Opus
		opus, err := processor.PCMToOpus(pcm)
		require.NoError(t, err)
		assert.NotEmpty(t, opus, "Opus output should not be empty")
	})
}

func TestAudioProcessor_EdgeCases(t *testing.T) {
	processor := createTestProcessor(t)
	defer processor.Close()

	t.Run("large_audio_chunks", func(t *testing.T) {
		// Test with 5 seconds of audio (should be at the limit)
		largePCM := generateTestPCM16(testSampleRate24k * 5) // 5 seconds
		
		base64Audio, err := processor.PCMToBase64(largePCM)
		assert.NoError(t, err)

		pcmBack, err := processor.Base64ToPCM(base64Audio)
		assert.NoError(t, err)
		assert.Equal(t, largePCM, pcmBack)
	})

	t.Run("very_large_audio_chunks", func(t *testing.T) {
		// Test with > 5 seconds (should fail validation)
		veryLargePCM := generateTestPCM16(testSampleRate24k * 6) // 6 seconds
		
		base64Audio, err := processor.PCMToBase64(veryLargePCM)
		require.NoError(t, err) // Encoding should work

		_, err = processor.Base64ToPCM(base64Audio)
		assert.Error(t, err, "Should reject audio chunks longer than 5 seconds")
		assert.Contains(t, err.Error(), "too long", "Error should mention audio is too long")
	})

	t.Run("minimal_audio_chunks", func(t *testing.T) {
		// Test with very short audio (2 samples = 1ms at 24kHz)
		shortPCM := generateTestPCM16(2)
		
		base64Audio, err := processor.PCMToBase64(shortPCM)
		require.NoError(t, err)

		_, err = processor.Base64ToPCM(base64Audio)
		assert.NoError(t, err, "Should handle very short audio")
	})
}

func TestAudioProcessor_ConcurrentAccess(t *testing.T) {
	processor := createTestProcessor(t)
	defer processor.Close()

	const numGoroutines = 10
	const numOperations = 100

	// Test concurrent operations
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- true }()
			
			for j := 0; j < numOperations; j++ {
				// Mix different operations
				switch j % 4 {
				case 0:
					processor.DetectSilence(generateTestPCM16(testFrameSize20ms))
				case 1:
					processor.PCMToBase64(generateTestPCM16(testFrameSize20ms))
				case 2:
					processor.Base64ToPCM(base64.StdEncoding.EncodeToString(generateTestPCM16(testFrameSize20ms)))
				case 3:
					processor.ConfigureQuality("medium")
				}
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
			// Success
		case <-time.After(10 * time.Second):
			t.Fatal("Concurrent test timed out")
		}
	}
}

// Helper functions

func createTestProcessor(t *testing.T) voice.AudioProcessor {
	logger := zaptest.NewLogger(t)
	cfg := &config.Config{
		Voice: config.VoiceConfig{
			AudioQuality:      "medium",
			SilenceThreshold:  0.01,
			SilenceDuration:   1500,
		},
	}

	processor, err := voice.NewAudioProcessor(logger, cfg)
	require.NoError(t, err, "Failed to create test audio processor")
	return processor
}

func generateMockOpusFrame() []byte {
	// Use a static well-formed Opus silence frame that's known to decode properly
	// This represents approximately 20ms of silence at 48kHz stereo
	return []byte{
		0xF8, 0xFF, 0xFE, // Opus frame header for silence
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20,
	}
}

func generateTestPCM16(samples int) []byte {
	// Generate 16-bit PCM audio with sine wave at 440Hz (A4 note)
	pcm := make([]byte, samples*2) // 2 bytes per 16-bit sample
	
	for i := 0; i < samples; i++ {
		// Generate sine wave
		t := float64(i) / float64(testSampleRate24k)
		amplitude := math.Sin(2 * math.Pi * 440 * t) * 0.5 // 50% amplitude
		sample := int16(amplitude * 32767)
		
		// Convert to little-endian bytes
		pcm[i*2] = byte(sample & 0xFF)
		pcm[i*2+1] = byte((sample >> 8) & 0xFF)
	}
	
	return pcm
}

func generateHighEnergyAudio(samples int) []byte {
	// Generate high energy audio (loud sine wave)
	pcm := make([]byte, samples*2)
	
	for i := 0; i < samples; i++ {
		t := float64(i) / float64(testSampleRate24k)
		amplitude := math.Sin(2 * math.Pi * 1000 * t) * 0.9 // 90% amplitude at 1kHz
		sample := int16(amplitude * 32767)
		
		pcm[i*2] = byte(sample & 0xFF)
		pcm[i*2+1] = byte((sample >> 8) & 0xFF)
	}
	
	return pcm
}

func generateLowNoiseAudio(samples int) []byte {
	// Generate low energy background noise
	pcm := make([]byte, samples*2)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	
	for i := 0; i < samples; i++ {
		// Generate random noise with low amplitude
		noise := (rng.Float64() - 0.5) * 0.01 // 1% amplitude noise
		sample := int16(noise * 32767)
		
		pcm[i*2] = byte(sample & 0xFF)
		pcm[i*2+1] = byte((sample >> 8) & 0xFF)
	}
	
	return pcm
}

// Benchmark tests
func BenchmarkAudioProcessor_OpusToPCM(b *testing.B) {
	processor := createBenchmarkProcessor(b)
	defer processor.Close()
	
	opusData := generateMockOpusFrame()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := processor.OpusToPCM(opusData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAudioProcessor_PCMToBase64(b *testing.B) {
	processor := createBenchmarkProcessor(b)
	defer processor.Close()
	
	pcm := generateTestPCM16(testFrameSize20ms)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := processor.PCMToBase64(pcm)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAudioProcessor_DetectSilence(b *testing.B) {
	processor := createBenchmarkProcessor(b)
	defer processor.Close()
	
	audio := generateTestPCM16(testFrameSize20ms)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		processor.DetectSilence(audio)
	}
}

func createBenchmarkProcessor(b *testing.B) voice.AudioProcessor {
	logger := zaptest.NewLogger(b)
	cfg := &config.Config{
		Voice: config.VoiceConfig{
			AudioQuality:      "medium",
			SilenceThreshold:  0.01,
			SilenceDuration:   1500,
		},
	}

	processor, err := voice.NewAudioProcessor(logger, cfg)
	if err != nil {
		b.Fatal("Failed to create benchmark audio processor:", err)
	}
	return processor
}