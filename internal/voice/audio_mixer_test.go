package voice_test

import (
	"math"
	"math/cmplx"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/Raikerian/go-discord-chatgpt/internal/voice"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/mjibson/go-dsp/fft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Test helpers
func createTestMixer(t testing.TB) voice.AudioMixer {
	logger := zap.NewNop()
	cfg := &config.Config{
		Voice: config.VoiceConfig{
			SampleRate: 24000,
		},
	}
	return voice.NewAudioMixer(logger, cfg)
}

// Signal generation utilities
func generateSineWave(frequency float64, amplitude float64, sampleRate int, duration time.Duration) []byte {
	samples := int(duration.Seconds() * float64(sampleRate))
	result := make([]byte, samples*2) // 16-bit samples

	for i := 0; i < samples; i++ {
		t := float64(i) / float64(sampleRate)
		value := amplitude * math.Sin(2*math.Pi*frequency*t)
		sample := int16(value * 32767)
		
		// Little-endian encoding
		result[i*2] = byte(sample)
		result[i*2+1] = byte(sample >> 8)
	}
	
	return result
}

func generateWhiteNoise(amplitude float64, sampleRate int, duration time.Duration) []byte {
	samples := int(duration.Seconds() * float64(sampleRate))
	result := make([]byte, samples*2)
	
	// Simple LCG for reproducible "random" noise
	var seed uint32 = 12345
	for i := 0; i < samples; i++ {
		seed = seed*1103515245 + 12345
		noise := (float64(seed) / float64(1<<32) - 0.5) * 2 * amplitude
		sample := int16(noise * 32767)
		
		result[i*2] = byte(sample)
		result[i*2+1] = byte(sample >> 8)
	}
	
	return result
}

func generateChirp(startFreq, endFreq float64, amplitude float64, sampleRate int, duration time.Duration) []byte {
	samples := int(duration.Seconds() * float64(sampleRate))
	result := make([]byte, samples*2)
	
	for i := 0; i < samples; i++ {
		t := float64(i) / float64(sampleRate)
		// Logarithmic frequency sweep
		freq := startFreq * math.Pow(endFreq/startFreq, t/duration.Seconds())
		phase := 2 * math.Pi * freq * t
		value := amplitude * math.Sin(phase)
		sample := int16(value * 32767)
		
		result[i*2] = byte(sample)
		result[i*2+1] = byte(sample >> 8)
	}
	
	return result
}

func generateSquareWave(amplitude float64, sampleRate int, duration time.Duration, frequency float64) []byte {
	samples := int(duration.Seconds() * float64(sampleRate))
	result := make([]byte, samples*2)
	
	period := float64(sampleRate) / frequency
	for i := 0; i < samples; i++ {
		value := amplitude
		if math.Mod(float64(i), period) >= period/2 {
			value = -amplitude
		}
		sample := int16(value * 32767)
		
		result[i*2] = byte(sample)
		result[i*2+1] = byte(sample >> 8)
	}
	
	return result
}

// Audio quality analysis functions

// AudioQualityAnalyzer performs FFT-based audio quality analysis
type AudioQualityAnalyzer struct {
	SampleRate int
	WindowSize int
}

// calculateSNR uses FFT to properly calculate Signal-to-Noise Ratio
func calculateSNR(signal []byte) float64 {
	analyzer := &AudioQualityAnalyzer{
		SampleRate: 24000,
		WindowSize: len(signal) / 2,
	}
	
	// Convert byte array to float64 for FFT
	samples := make([]float64, len(signal)/2)
	for i := 0; i < len(samples); i++ {
		sample := int16(uint16(signal[i*2]) | (uint16(signal[i*2+1]) << 8))
		samples[i] = float64(sample) / 32768.0
	}
	
	return analyzer.CalculateSNR(samples)
}

// CalculateSNR calculates Signal-to-Noise Ratio using FFT
func (aq *AudioQualityAnalyzer) CalculateSNR(signal []float64) float64 {
	// Apply FFT to separate signal and noise components
	fftData := fft.FFTReal(signal)
	
	// Calculate magnitude spectrum
	magnitudes := make([]float64, len(fftData))
	for i, c := range fftData {
		magnitudes[i] = cmplx.Abs(c)
	}
	
	// Identify signal peaks and noise floor
	signalPower := aq.calculateSignalPower(magnitudes)
	noisePower := aq.calculateNoisePower(magnitudes)
	
	if noisePower == 0 {
		return 100.0 // Perfect signal
	}
	
	// Return SNR in dB
	return 20 * math.Log10(signalPower/noisePower)
}

// calculateSignalPower identifies signal peaks in frequency domain
func (aq *AudioQualityAnalyzer) calculateSignalPower(magnitudes []float64) float64 {
	// Find the peak magnitude (likely our test signal)
	maxMag := 0.0
	peakIdx := 0
	
	// Search in the first half (positive frequencies)
	for i := 1; i < len(magnitudes)/2; i++ {
		if magnitudes[i] > maxMag {
			maxMag = magnitudes[i]
			peakIdx = i
		}
	}
	
	// Calculate power around the peak (±10 bins)
	signalPower := 0.0
	startBin := peakIdx - 10
	if startBin < 0 {
		startBin = 0
	}
	endBin := peakIdx + 10
	if endBin > len(magnitudes)/2 {
		endBin = len(magnitudes) / 2
	}
	
	for i := startBin; i <= endBin; i++ {
		signalPower += magnitudes[i] * magnitudes[i]
	}
	
	return math.Sqrt(signalPower)
}

// calculateNoisePower estimates noise floor from frequency domain
func (aq *AudioQualityAnalyzer) calculateNoisePower(magnitudes []float64) float64 {
	// Sort magnitudes to find noise floor (lower percentiles)
	sorted := make([]float64, len(magnitudes)/2)
	copy(sorted, magnitudes[:len(magnitudes)/2])
	sort.Float64s(sorted)
	
	// Use 20th percentile as noise floor estimate
	noiseFloorIdx := len(sorted) * 20 / 100
	if noiseFloorIdx == 0 {
		noiseFloorIdx = 1
	}
	
	// Calculate average noise power from lower percentiles
	noisePower := 0.0
	count := 0
	for i := 0; i < noiseFloorIdx; i++ {
		if sorted[i] > 0 {
			noisePower += sorted[i] * sorted[i]
			count++
		}
	}
	
	if count == 0 {
		return 0.0001 // Minimal noise floor
	}
	
	return math.Sqrt(noisePower / float64(count))
}

// calculateTHD uses FFT to calculate Total Harmonic Distortion
func calculateTHD(signal []byte, fundamentalFreq float64, sampleRate int) float64 {
	analyzer := &AudioQualityAnalyzer{
		SampleRate: sampleRate,
		WindowSize: len(signal) / 2,
	}
	
	// Convert byte array to float64 for FFT
	samples := make([]float64, len(signal)/2)
	for i := 0; i < len(samples); i++ {
		sample := int16(uint16(signal[i*2]) | (uint16(signal[i*2+1]) << 8))
		samples[i] = float64(sample) / 32768.0
	}
	
	return analyzer.MeasureTHD(samples, fundamentalFreq)
}

// MeasureTHD measures Total Harmonic Distortion using FFT
func (aq *AudioQualityAnalyzer) MeasureTHD(signal []float64, fundamental float64) float64 {
	// Apply FFT
	fftData := fft.FFTReal(signal)
	
	// Calculate magnitude spectrum
	magnitudes := make([]float64, len(fftData))
	for i, c := range fftData {
		magnitudes[i] = cmplx.Abs(c)
	}
	
	// Get power at fundamental and harmonic frequencies
	fundamentalPower := aq.getPowerAtFrequency(magnitudes, fundamental)
	harmonicPower := 0.0
	
	// Sum power at harmonic frequencies (2nd through 5th harmonics)
	for harmonic := 2; harmonic <= 5; harmonic++ {
		freq := fundamental * float64(harmonic)
		harmonicPower += aq.getPowerAtFrequency(magnitudes, freq)
	}
	
	if fundamentalPower == 0 {
		return 1.0 // 100% distortion if no fundamental
	}
	
	// THD as percentage
	return math.Sqrt(harmonicPower/fundamentalPower) * 100
}

// getPowerAtFrequency extracts power at a specific frequency from FFT data
func (aq *AudioQualityAnalyzer) getPowerAtFrequency(magnitudes []float64, frequency float64) float64 {
	// Calculate bin index for the frequency
	binIndex := int(frequency * float64(len(magnitudes)) / float64(aq.SampleRate))
	
	if binIndex < 0 || binIndex >= len(magnitudes)/2 {
		return 0.0
	}
	
	// Sum power in a narrow band around the target frequency (±2 bins)
	power := 0.0
	startBin := binIndex - 2
	if startBin < 0 {
		startBin = 0
	}
	endBin := binIndex + 2
	if endBin >= len(magnitudes)/2 {
		endBin = len(magnitudes)/2 - 1
	}
	
	for i := startBin; i <= endBin; i++ {
		power += magnitudes[i] * magnitudes[i]
	}
	
	return power
}

func calculateRMS(audio []byte) float64 {
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
	
	return math.Sqrt(sum / float64(sampleCount))
}

// Test cases

func TestAudioQuality(t *testing.T) {
	t.Run("SNR_Calculation", func(t *testing.T) {
		// Generate clean sine wave
		signal := generateSineWave(440, 0.5, 24000, 100*time.Millisecond)
		snr := calculateSNR(signal)
		
		// Should have high SNR for clean signal
		assert.Greater(t, snr, 80.0, "SNR should be > 80dB for clean signal")
	})
	
	t.Run("THD_Measurement", func(t *testing.T) {
		// Generate sine wave
		signal := generateSineWave(440, 0.5, 24000, 100*time.Millisecond)
		thd := calculateTHD(signal, 440, 24000)
		
		// Should have low THD (now returned as percentage)
		assert.Less(t, thd, 0.1, "THD should be < 0.1% for pure sine")
	})
}

func TestMultiStreamSync(t *testing.T) {
	mixer := createTestMixer(t)
	
	// Generate synchronized test signals (click tracks)
	duration := 100 * time.Millisecond
	clickTrack1 := generateSquareWave(0.5, 24000, duration, 4) // 4 Hz click
	clickTrack2 := generateSquareWave(0.5, 24000, duration, 4) // Should align
	
	user1 := discord.UserID(1)
	user2 := discord.UserID(2)
	
	// Add audio with proper RTP timestamps
	baseTime := time.Now()
	rtpTimestamp := uint32(0)
	sequence := uint16(0)
	
	// Add packets for both users simultaneously
	packetSize := 960 // 20ms at 24kHz = 480 samples * 2 bytes
	for offset := 0; offset+packetSize <= len(clickTrack1); offset += packetSize {
		packet1 := clickTrack1[offset : offset+packetSize]
		packet2 := clickTrack2[offset : offset+packetSize]
		
		timestamp := baseTime.Add(time.Duration(offset/2) * time.Second / 24000)
		
		err := mixer.AddUserAudioWithRTP(user1, packet1, timestamp, rtpTimestamp, sequence)
		require.NoError(t, err)
		
		err = mixer.AddUserAudioWithRTP(user2, packet2, timestamp, rtpTimestamp, sequence)
		require.NoError(t, err)
		
		rtpTimestamp += 960 // RTP timestamp increment for 20ms at 48kHz
		sequence++
	}
	
	// Get mixed audio
	mixed, err := mixer.GetMixedAudio(duration)
	require.NoError(t, err)
	
	// Verify synchronization - mixed signal should have correlated peaks
	mixedRMS := calculateRMS(mixed)
	assert.Greater(t, mixedRMS, 0.0, "Mixed signal should have content")
	
	// In a real test, would calculate cross-correlation
	// For now, verify basic properties
	assert.Equal(t, len(clickTrack1), len(mixed), "Output length should match input")
}

func TestMixingEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		scenario func() (voice.AudioMixer, []discord.UserID, time.Duration)
		validate func(t *testing.T, output []byte)
	}{
		{
			name: "silence_handling",
			scenario: func() (voice.AudioMixer, []discord.UserID, time.Duration) {
				mixer := createTestMixer(t)
				user1 := discord.UserID(1)
				user2 := discord.UserID(2)
				
				silence := make([]byte, 960) // 20ms of silence
				signal := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
				
				// Add silence from user1, signal from user2
				timestamp := time.Now()
				mixer.AddUserAudioWithRTP(user1, silence, timestamp, 0, 0)
				mixer.AddUserAudioWithRTP(user2, signal, timestamp, 0, 0)
				
				return mixer, []discord.UserID{user1, user2}, 20 * time.Millisecond
			},
			validate: func(t *testing.T, output []byte) {
				// Output should have signal content (not silence)
				rms := calculateRMS(output)
				assert.Greater(t, rms, 0.1, "Output should contain signal, not silence")
			},
		},
		{
			name: "clipping_prevention",
			scenario: func() (voice.AudioMixer, []discord.UserID, time.Duration) {
				mixer := createTestMixer(t)
				user1 := discord.UserID(1)
				user2 := discord.UserID(2)
				
				duration := 20 * time.Millisecond
				// Generate max amplitude signals
				maxSignal1 := generateSquareWave(1.0, 24000, duration, 440)
				maxSignal2 := generateSquareWave(1.0, 24000, duration, 440)
				
				timestamp := time.Now()
				mixer.AddUserAudioWithRTP(user1, maxSignal1, timestamp, 0, 0)
				mixer.AddUserAudioWithRTP(user2, maxSignal2, timestamp, 0, 0)
				
				return mixer, []discord.UserID{user1, user2}, duration
			},
			validate: func(t *testing.T, output []byte) {
				// Check no samples exceed [-1, 1] range
				samples := len(output) / 2
				for i := 0; i < samples; i++ {
					sample := int16(uint16(output[i*2]) | (uint16(output[i*2+1]) << 8))
					normalizedSample := float64(sample) / 32768.0
					assert.LessOrEqual(t, math.Abs(normalizedSample), 1.0,
						"Sample %d clipped: %f", i, normalizedSample)
				}
			},
		},
		{
			name: "empty_buffer_handling",
			scenario: func() (voice.AudioMixer, []discord.UserID, time.Duration) {
				mixer := createTestMixer(t)
				// Don't add any audio
				return mixer, []discord.UserID{}, 100 * time.Millisecond
			},
			validate: func(t *testing.T, output []byte) {
				// Should return silence
				assert.NotNil(t, output)
				rms := calculateRMS(output)
				assert.Equal(t, 0.0, rms, "Empty buffer should return silence")
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mixer, _, testDuration := tt.scenario()
			output, err := mixer.GetMixedAudio(testDuration)
			require.NoError(t, err)
			tt.validate(t, output)
		})
	}
}

func TestRealTimePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	
	mixer := createTestMixer(t)
	// bufferSize := 960      // 20ms at 48kHz, converted to 24kHz
	bufferPeriod := 20 * time.Millisecond
	numStreams := 8
	
	// Generate test streams
	testStreams := make([][]byte, numStreams)
	for i := 0; i < numStreams; i++ {
		freq := 200.0 + float64(i)*100.0 // Different frequency for each stream
		testStreams[i] = generateSineWave(freq, 0.3, 24000, bufferPeriod)
	}
	
	// Measure latencies
	latencies := make([]time.Duration, 1000)
	timestamp := time.Now()
	rtpTimestamp := uint32(0)
	
	for i := range latencies {
		start := time.Now()
		
		// Add audio from all streams
		for j := 0; j < numStreams; j++ {
			userID := discord.UserID(j + 1)
			err := mixer.AddUserAudioWithRTP(userID, testStreams[j], timestamp, rtpTimestamp, uint16(i))
			require.NoError(t, err)
		}
		
		// Get mixed output
		_, err := mixer.GetMixedAudio(bufferPeriod)
		require.NoError(t, err)
		
		latencies[i] = time.Since(start)
		timestamp = timestamp.Add(bufferPeriod)
		rtpTimestamp += 960
	}
	
	// Calculate 99th percentile
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})
	
	p99 := latencies[990]
	assert.Less(t, p99, bufferPeriod/2,
		"Processing time exceeds 50%% of buffer period: %v", p99)
	
	// Log performance stats
	t.Logf("Performance stats - P50: %v, P95: %v, P99: %v",
		latencies[500], latencies[950], p99)
}

func TestJitterBufferBehavior(t *testing.T) {
	t.Run("packet_reordering", func(t *testing.T) {
		mixer := createTestMixer(t)
		user := discord.UserID(1)
		
		// Generate packets
		packets := make([][]byte, 5)
		for i := range packets {
			packets[i] = generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
		}
		
		// Add packets out of order
		timestamp := time.Now()
		baseRTP := uint32(0)
		
		// Add in order: 2, 0, 4, 1, 3
		order := []int{2, 0, 4, 1, 3}
		for _, idx := range order {
			err := mixer.AddUserAudioWithRTP(user, packets[idx], 
				timestamp.Add(time.Duration(idx)*20*time.Millisecond),
				baseRTP+uint32(idx)*960, uint16(idx))
			require.NoError(t, err)
		}
		
		// Get mixed audio - should be properly ordered
		mixed, err := mixer.GetMixedAudio(100 * time.Millisecond)
		require.NoError(t, err)
		assert.NotNil(t, mixed)
		assert.Greater(t, len(mixed), 0)
	})
	
	t.Run("packet_loss_handling", func(t *testing.T) {
		mixer := createTestMixer(t)
		user := discord.UserID(1)
		
		// Add packets with gaps (simulating packet loss)
		timestamp := time.Now()
		baseRTP := uint32(0)
		
		// Add packets 0, 2, 3, 5 (missing 1 and 4)
		sequences := []uint16{0, 2, 3, 5}
		for _, seq := range sequences {
			audio := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
			err := mixer.AddUserAudioWithRTP(user, audio,
				timestamp.Add(time.Duration(seq)*20*time.Millisecond),
				baseRTP+uint32(seq)*960, seq)
			require.NoError(t, err)
		}
		
		// Get mixed audio - should handle gaps gracefully
		mixed, err := mixer.GetMixedAudio(120 * time.Millisecond)
		require.NoError(t, err)
		assert.NotNil(t, mixed)
		
		// Verify expected size despite packet loss
		expectedSamples := int(120 * time.Millisecond * 24000 / time.Second)
		expectedBytes := expectedSamples * 2
		assert.Equal(t, expectedBytes, len(mixed))
	})
}

func TestRTPTimestampWraparound(t *testing.T) {
	mixer := createTestMixer(t)
	user := discord.UserID(1)
	
	// Test RTP timestamp wraparound (32-bit overflow)
	maxRTP := uint32(math.MaxUint32 - 1000)
	timestamp := time.Now()
	
	// Add packet just before wraparound
	audio1 := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
	err := mixer.AddUserAudioWithRTP(user, audio1, timestamp, maxRTP, 1)
	require.NoError(t, err)
	
	// Add packet after wraparound
	audio2 := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
	wrappedRTP := uint32(100) // Wrapped around
	err = mixer.AddUserAudioWithRTP(user, audio2, timestamp.Add(20*time.Millisecond), wrappedRTP, 2)
	require.NoError(t, err)
	
	// Should handle wraparound correctly
	mixed, err := mixer.GetMixedAudio(40 * time.Millisecond)
	require.NoError(t, err)
	assert.NotNil(t, mixed)
}

func TestDominantSpeakerDetection(t *testing.T) {
	mixer := createTestMixer(t)
	
	// Add quiet audio from user 1
	user1 := discord.UserID(1)
	quietAudio := generateSineWave(440, 0.1, 24000, 100*time.Millisecond)
	
	// Add loud audio from user 2
	user2 := discord.UserID(2)
	loudAudio := generateSineWave(880, 0.8, 24000, 100*time.Millisecond)
	
	timestamp := time.Now()
	
	// Add in 20ms chunks
	for offset := 0; offset < len(quietAudio); offset += 960 {
		if offset+960 <= len(quietAudio) {
			mixer.AddUserAudioWithRTP(user1, quietAudio[offset:offset+960], 
				timestamp, uint32(offset/2), uint16(offset/960))
			mixer.AddUserAudioWithRTP(user2, loudAudio[offset:offset+960], 
				timestamp, uint32(offset/2), uint16(offset/960))
		}
		timestamp = timestamp.Add(20 * time.Millisecond)
	}
	
	// Check dominant speaker
	dominantUser, confidence := mixer.GetDominantSpeaker()
	assert.Equal(t, user2, dominantUser, "User 2 should be dominant speaker")
	assert.Greater(t, confidence, float32(0.7), "Confidence should be high")
}

func TestConcurrentAccess(t *testing.T) {
	mixer := createTestMixer(t)
	
	// Test concurrent adds and reads
	var wg sync.WaitGroup
	numGoroutines := 10
	numPacketsEach := 50
	
	wg.Add(numGoroutines * 2) // Writers and readers
	
	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		go func(userID int) {
			defer wg.Done()
			
			user := discord.UserID(userID)
			timestamp := time.Now()
			
			for j := 0; j < numPacketsEach; j++ {
				audio := generateSineWave(440+float64(userID)*50, 0.3, 24000, 20*time.Millisecond)
				err := mixer.AddUserAudioWithRTP(user, audio, timestamp, uint32(j*960), uint16(j))
				assert.NoError(t, err)
				
				timestamp = timestamp.Add(20 * time.Millisecond)
				time.Sleep(time.Millisecond) // Simulate real timing
			}
		}(i)
	}
	
	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			
			for j := 0; j < numPacketsEach; j++ {
				mixed, err := mixer.GetMixedAudio(20 * time.Millisecond)
				assert.NoError(t, err)
				assert.NotNil(t, mixed)
				
				time.Sleep(time.Millisecond)
			}
		}()
	}
	
	wg.Wait()
}

func TestGetAllAvailableMixedAudio(t *testing.T) {
	mixer := createTestMixer(t)
	
	// Add audio from multiple users with different durations
	user1 := discord.UserID(1)
	user2 := discord.UserID(2)
	
	timestamp := time.Now()
	
	// User 1: 200ms of audio
	for i := 0; i < 10; i++ {
		audio := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
		err := mixer.AddUserAudioWithRTP(user1, audio, 
			timestamp.Add(time.Duration(i)*20*time.Millisecond),
			uint32(i*960), uint16(i))
		require.NoError(t, err)
	}
	
	// User 2: 100ms of audio, starting 50ms later
	startTime := timestamp.Add(50 * time.Millisecond)
	for i := 0; i < 5; i++ {
		audio := generateSineWave(880, 0.5, 24000, 20*time.Millisecond)
		err := mixer.AddUserAudioWithRTP(user2, audio,
			startTime.Add(time.Duration(i)*20*time.Millisecond),
			uint32(i*960), uint16(i))
		require.NoError(t, err)
	}
	
	// Get all available audio
	mixed, duration, err := mixer.GetAllAvailableMixedAudio()
	require.NoError(t, err)
	
	// Should return ~200ms of audio (from earliest to latest packet)
	assert.InDelta(t, 200*time.Millisecond, duration, float64(30*time.Millisecond))
	
	// Verify audio size matches duration
	expectedSamples := int(duration * 24000 / time.Second)
	expectedBytes := expectedSamples * 2
	assert.Equal(t, expectedBytes, len(mixed))
}

func TestGetAllAvailableMixedAudioAndFlush(t *testing.T) {
	mixer := createTestMixer(t)
	
	// Add some audio
	user := discord.UserID(1)
	timestamp := time.Now()
	
	for i := 0; i < 5; i++ {
		audio := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
		err := mixer.AddUserAudioWithRTP(user, audio,
			timestamp.Add(time.Duration(i)*20*time.Millisecond),
			uint32(i*960), uint16(i))
		require.NoError(t, err)
	}
	
	// Get and flush
	mixed, duration, err := mixer.GetAllAvailableMixedAudioAndFlush()
	require.NoError(t, err)
	assert.Greater(t, len(mixed), 0)
	assert.Greater(t, duration, time.Duration(0))
	
	// Verify buffers are flushed
	mixed2, duration2, err := mixer.GetAllAvailableMixedAudio()
	require.NoError(t, err)
	assert.Equal(t, 0, len(mixed2))
	assert.Equal(t, time.Duration(0), duration2)
}

func TestClearUserBuffer(t *testing.T) {
	mixer := createTestMixer(t)
	
	user1 := discord.UserID(1)
	user2 := discord.UserID(2)
	
	// Add audio for both users
	timestamp := time.Now()
	audio := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
	
	mixer.AddUserAudioWithRTP(user1, audio, timestamp, 0, 0)
	mixer.AddUserAudioWithRTP(user2, audio, timestamp, 0, 0)
	
	// Clear user1's buffer
	mixer.ClearUserBuffer(user1)
	
	// Get mixed audio - should only have user2
	mixed, err := mixer.GetMixedAudio(20 * time.Millisecond)
	require.NoError(t, err)
	
	// Verify user2's audio is still there
	rms := calculateRMS(mixed)
	assert.Greater(t, rms, 0.0, "Should still have user2's audio")
}

func TestClearAllBuffers(t *testing.T) {
	mixer := createTestMixer(t)
	
	// Add audio from multiple users
	timestamp := time.Now()
	for i := 0; i < 3; i++ {
		user := discord.UserID(i + 1)
		audio := generateSineWave(440+float64(i)*100, 0.5, 24000, 20*time.Millisecond)
		mixer.AddUserAudioWithRTP(user, audio, timestamp, 0, uint16(i))
	}
	
	// Clear all buffers
	mixer.ClearAllBuffers()
	
	// Get mixed audio - should be silence
	mixed, err := mixer.GetMixedAudio(20 * time.Millisecond)
	require.NoError(t, err)
	
	rms := calculateRMS(mixed)
	assert.Equal(t, 0.0, rms, "Should return silence after clearing all buffers")
}

// Benchmarks

func BenchmarkMixingAlgorithms(b *testing.B) {
	// Create test streams
	numStreams := 5
	duration := 20 * time.Millisecond
	streams := make([][]byte, numStreams)
	
	for i := 0; i < numStreams; i++ {
		freq := 200 + float64(i)*100
		streams[i] = generateSineWave(freq, 0.5, 24000, duration)
	}
	
	b.Run("current_implementation", func(b *testing.B) {
		mixer := createTestMixer(b)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Add streams
			timestamp := time.Now()
			for j, stream := range streams {
				mixer.AddUserAudioWithRTP(discord.UserID(j+1), stream, timestamp, 0, 0)
			}
			
			// Mix
			_, _ = mixer.GetMixedAudio(duration)
		}
	})
}

func BenchmarkJitterBuffer(b *testing.B) {
	mixer := createTestMixer(b)
	user := discord.UserID(1)
	
	// Generate test packets
	packets := make([][]byte, 100)
	for i := range packets {
		packets[i] = generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		timestamp := time.Now()
		for j, packet := range packets {
			mixer.AddUserAudioWithRTP(user, packet, 
				timestamp.Add(time.Duration(j)*20*time.Millisecond),
				uint32(j*960), uint16(j))
		}
		
		mixer.ClearUserBuffer(user)
	}
}

func BenchmarkGetMixedAudio(b *testing.B) {
	mixer := createTestMixer(b)
	
	// Pre-populate with audio from multiple users
	numUsers := 8
	timestamp := time.Now()
	
	for i := 0; i < numUsers; i++ {
		user := discord.UserID(i + 1)
		for j := 0; j < 50; j++ { // 1 second of audio
			audio := generateSineWave(440+float64(i)*50, 0.3, 24000, 20*time.Millisecond)
			mixer.AddUserAudioWithRTP(user, audio,
				timestamp.Add(time.Duration(j)*20*time.Millisecond),
				uint32(j*960), uint16(j))
		}
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mixer.GetMixedAudio(100 * time.Millisecond)
	}
}

// Integration test combining multiple features
func TestIntegrationMultiUserScenario(t *testing.T) {
	mixer := createTestMixer(t)
	
	// Simulate a conversation with 3 users
	user1 := discord.UserID(1) // Speaks first
	user2 := discord.UserID(2) // Joins mid-conversation
	user3 := discord.UserID(3) // Speaks briefly
	
	baseTime := time.Now()
	
	// User 1 starts speaking
	for i := 0; i < 10; i++ {
		audio := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
		timestamp := baseTime.Add(time.Duration(i) * 20 * time.Millisecond)
		mixer.AddUserAudioWithRTP(user1, audio, timestamp, uint32(i*960), uint16(i))
	}
	
	// User 2 joins after 100ms
	for i := 0; i < 15; i++ {
		audio := generateSineWave(880, 0.4, 24000, 20*time.Millisecond)
		timestamp := baseTime.Add(100*time.Millisecond + time.Duration(i)*20*time.Millisecond)
		mixer.AddUserAudioWithRTP(user2, audio, timestamp, uint32(i*960), uint16(i))
	}
	
	// User 3 speaks briefly at 200ms
	for i := 0; i < 5; i++ {
		audio := generateSineWave(660, 0.6, 24000, 20*time.Millisecond)
		timestamp := baseTime.Add(200*time.Millisecond + time.Duration(i)*20*time.Millisecond)
		mixer.AddUserAudioWithRTP(user3, audio, timestamp, uint32(i*960), uint16(i))
	}
	
	// Get all mixed audio
	mixed, duration, err := mixer.GetAllAvailableMixedAudio()
	require.NoError(t, err)
	
	// Should cover the full conversation duration
	expectedDuration := 400 * time.Millisecond // User 2 speaks until 400ms
	assert.InDelta(t, expectedDuration, duration, float64(50*time.Millisecond))
	
	// Verify mixed audio properties
	rms := calculateRMS(mixed)
	assert.Greater(t, rms, 0.0, "Mixed audio should have content")
	
	// Check dominant speaker at end (should be user2)
	dominant, confidence := mixer.GetDominantSpeaker()
	
	// User 2 should be dominant because they have the highest cumulative energy:
	// User 1: 10 packets * ~0.354 RMS = ~3.54 total energy
	// User 2: 15 packets * ~0.283 RMS = ~4.24 total energy  
	// User 3: 5 packets * ~0.424 RMS = ~2.12 total energy
	assert.Equal(t, user2, dominant, "User 2 should be dominant (highest cumulative energy)")
	assert.Greater(t, confidence, float32(0.4), "Confidence should be > 0.4")
}

// Test mixing with varying packet sizes
func TestVariablePacketSizes(t *testing.T) {
	mixer := createTestMixer(t)
	user := discord.UserID(1)
	
	// Different packet sizes (simulating variable bitrate or network conditions)
	packetSizes := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		20 * time.Millisecond,
		10 * time.Millisecond,
	}
	
	timestamp := time.Now()
	rtpTimestamp := uint32(0)
	
	for i, size := range packetSizes {
		audio := generateSineWave(440, 0.5, 24000, size)
		err := mixer.AddUserAudioWithRTP(user, audio, timestamp, rtpTimestamp, uint16(i))
		require.NoError(t, err)
		
		timestamp = timestamp.Add(size)
		rtpTimestamp += uint32(size.Milliseconds() * 48) // 48 samples per ms at 48kHz
	}
	
	// Get mixed audio for total duration
	totalDuration := 90 * time.Millisecond
	mixed, err := mixer.GetMixedAudio(totalDuration)
	require.NoError(t, err)
	
	// Verify we got the expected amount
	expectedSamples := int(totalDuration * 24000 / time.Second)
	expectedBytes := expectedSamples * 2
	assert.Equal(t, expectedBytes, len(mixed))
}

// Test extreme edge cases
func TestExtremeEdgeCases(t *testing.T) {
	t.Run("very_short_duration", func(t *testing.T) {
		mixer := createTestMixer(t)
		
		// Request 1ms of audio
		mixed, err := mixer.GetMixedAudio(1 * time.Millisecond)
		require.NoError(t, err)
		
		// Should return correct size even for tiny duration
		expectedSamples := 24 // 1ms at 24kHz
		expectedBytes := expectedSamples * 2
		assert.Equal(t, expectedBytes, len(mixed))
	})
	
	t.Run("very_long_duration", func(t *testing.T) {
		mixer := createTestMixer(t)
		
		// Add only 100ms of audio
		user := discord.UserID(1)
		timestamp := time.Now()
		for i := 0; i < 5; i++ {
			audio := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
			mixer.AddUserAudioWithRTP(user, audio, timestamp, uint32(i*960), uint16(i))
			timestamp = timestamp.Add(20 * time.Millisecond)
		}
		
		// Request 10 seconds
		mixed, err := mixer.GetMixedAudio(10 * time.Second)
		require.NoError(t, err)
		
		// Should return requested size with padding
		expectedSamples := 10 * 24000
		expectedBytes := expectedSamples * 2
		assert.Equal(t, expectedBytes, len(mixed))
		
		// Most should be silence
		// Check that only the first ~100ms has content
		firstPartBytes := 100 * 24000 / 1000 * 2 // 100ms worth
		firstPartRMS := calculateRMS(mixed[:firstPartBytes])
		remainderRMS := calculateRMS(mixed[firstPartBytes:])
		
		assert.Greater(t, firstPartRMS, 0.1, "First part should have audio")
		assert.Less(t, remainderRMS, 0.01, "Remainder should be mostly silence")
	})
	
	t.Run("zero_duration", func(t *testing.T) {
		mixer := createTestMixer(t)
		
		mixed, err := mixer.GetMixedAudio(0)
		require.NoError(t, err)
		assert.Equal(t, 0, len(mixed))
	})
	
	t.Run("negative_duration", func(t *testing.T) {
		mixer := createTestMixer(t)
		
		// Should handle gracefully
		mixed, err := mixer.GetMixedAudio(-1 * time.Second)
		require.NoError(t, err)
		assert.Equal(t, 0, len(mixed))
	})
}

// Test buffer overflow protection
func TestBufferOverflowProtection(t *testing.T) {
	mixer := createTestMixer(t)
	user := discord.UserID(1)
	
	// Add many packets to test buffer limits
	timestamp := time.Now()
	audio := generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
	
	// Add 1000 packets (20 seconds of audio)
	for i := 0; i < 1000; i++ {
		err := mixer.AddUserAudioWithRTP(user, audio,
			timestamp.Add(time.Duration(i)*20*time.Millisecond),
			uint32(i*960), uint16(i%65536)) // Handle sequence wrap
		require.NoError(t, err)
	}
	
	// Should still work without memory issues
	mixed, duration, err := mixer.GetAllAvailableMixedAudio()
	require.NoError(t, err)
	
	// Should be capped at reasonable duration
	assert.LessOrEqual(t, duration, 30*time.Second, "Duration should be capped")
	assert.Greater(t, len(mixed), 0)
}

// Helper function to simulate network jitter
func TestNetworkJitter(t *testing.T) {
	mixer := createTestMixer(t)
	user := discord.UserID(1)
	
	// Generate packets with simulated jitter
	baseTime := time.Now()
	packets := make([]struct {
		audio     []byte
		timestamp time.Time
		rtp       uint32
		seq       uint16
	}, 20)
	
	// Create packets with proper timing
	for i := range packets {
		packets[i].audio = generateSineWave(440, 0.5, 24000, 20*time.Millisecond)
		packets[i].timestamp = baseTime.Add(time.Duration(i) * 20 * time.Millisecond)
		packets[i].rtp = uint32(i * 960)
		packets[i].seq = uint16(i)
	}
	
	// Shuffle to simulate out-of-order delivery
	// Simple shuffle for first 10 packets
	for i := 0; i < 10; i++ {
		j := i + 1 + i%(10-i)
		if j < 10 {
			packets[i], packets[j] = packets[j], packets[i]
		}
	}
	
	// Add packets with jittered arrival times
	for _, p := range packets {
		// Add 0-50ms jitter to arrival time
		jitter := time.Duration(p.seq%5) * 10 * time.Millisecond
		arrivalTime := p.timestamp.Add(jitter)
		
		err := mixer.AddUserAudioWithRTP(user, p.audio, arrivalTime, p.rtp, p.seq)
		require.NoError(t, err)
	}
	
	// Get mixed audio - should be properly ordered despite jitter
	mixed, err := mixer.GetMixedAudio(400 * time.Millisecond)
	require.NoError(t, err)
	
	// Verify output quality
	rms := calculateRMS(mixed)
	assert.Greater(t, rms, 0.3, "Should have consistent audio despite jitter")
}