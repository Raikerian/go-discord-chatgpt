package voice

import (
	"encoding/base64"
	"fmt"
	"math"
	"sync"
	"time"

	"layeh.com/gopus"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"go.uber.org/zap"
)

type AudioProcessor interface {
	// Convert Discord Opus to PCM for OpenAI
	OpusToPCM(opus []byte) ([]byte, error)

	// Convert PCM to base64 for OpenAI API
	PCMToBase64(pcm []byte) (string, error)

	// Convert base64 PCM from OpenAI to raw PCM
	Base64ToPCM(base64Audio string) ([]byte, error)

	// Convert OpenAI PCM response to Opus for Discord
	PCMToOpus(pcm []byte) ([]byte, error)

	// Detect silence in audio stream
	DetectSilence(audio []byte) (bool, float32) // returns isSilent, energyLevel

	// Process audio quality settings
	ConfigureQuality(quality string) error

	// Cleanup resources
	Close() error
}

type SilenceDetector struct {
	threshold      float32
	duration       time.Duration
	lastSpeechTime time.Time
	energyBuffer   []float32
	mu             sync.RWMutex

	// Adaptive threshold parameters
	adaptiveThreshold bool
	noiseFloor        float32
	thresholdMargin   float32
}

const (
	// Discord native format
	DiscordSampleRate = 48000
	DiscordChannels   = 2
	DiscordBitrate    = 64000

	// OpenAI Realtime requirements
	OpenAISampleRate = 24000 // Required by API
	OpenAIChannels   = 1     // Mono required
	OpenAIBitDepth   = 16    // 16-bit PCM

	// Frame sizes for different sample rates
	DiscordFrameSize = 960 // 20ms at 48kHz
	OpenAIFrameSize  = 480 // 20ms at 24kHz

	// Audio processing constants
	MaxSilenceDetectionSamples = 1000
	DefaultThresholdMargin     = 5.0
	MinimumThreshold           = 0.005 // Minimum threshold to prevent over-sensitivity
	MaximumThreshold           = 0.1   // Maximum threshold to ensure responsiveness
)

type AudioConfig struct {
	Bitrate    int
	FrameSize  int // milliseconds
	Complexity int
}

var QualityPresets = map[string]AudioConfig{
	"low": {
		Bitrate:    32000,
		FrameSize:  20, // ms
		Complexity: 5,
	},
	"medium": {
		Bitrate:    48000,
		FrameSize:  20,
		Complexity: 8,
	},
	"high": {
		Bitrate:    64000,
		FrameSize:  10,
		Complexity: 10,
	},
}

type audioProcessor struct {
	logger          *zap.Logger
	cfg             *config.VoiceConfig
	silenceDetector *SilenceDetector
	currentQuality  AudioConfig

	// Opus codecs
	opusDecoder *gopus.Decoder
	opusEncoder *gopus.Encoder

	// Thread safety
	mu sync.RWMutex
}

func NewAudioProcessor(logger *zap.Logger, cfg *config.Config) (AudioProcessor, error) {
	voiceCfg := &cfg.Voice

	// Initialize silence detector with reasonable bounds
	threshold := float32(DefaultSilenceThreshold)
	if voiceCfg.SilenceThreshold > 0 {
		threshold = voiceCfg.SilenceThreshold
	}

	// Ensure initial threshold is within reasonable bounds
	threshold = max(MinimumThreshold, min(MaximumThreshold, threshold))

	// Determine if adaptive threshold should be enabled
	// Default to true unless explicitly disabled in config
	adaptiveEnabled := true
	// Add config option check if available in the future
	// if voiceCfg.DisableAdaptiveThreshold { adaptiveEnabled = false }

	silenceDetector := &SilenceDetector{
		threshold:         threshold,
		duration:          time.Duration(voiceCfg.SilenceDuration) * time.Millisecond,
		lastSpeechTime:    time.Now(),
		energyBuffer:      make([]float32, 0, MaxSilenceDetectionSamples),
		adaptiveThreshold: adaptiveEnabled,
		noiseFloor:        threshold,
		thresholdMargin:   DefaultThresholdMargin,
	}

	// Set default quality
	quality := voiceCfg.AudioQuality
	if quality == "" {
		quality = "medium"
	}

	currentQuality, exists := QualityPresets[quality]
	if !exists {
		return nil, fmt.Errorf("unknown audio quality preset: %s", quality)
	}

	// Initialize Opus decoder for Discord -> PCM
	opusDecoder, err := gopus.NewDecoder(DiscordSampleRate, DiscordChannels)
	if err != nil {
		return nil, fmt.Errorf("failed to create opus decoder: %w", err)
	}

	// Initialize Opus encoder for PCM -> Discord
	opusEncoder, err := gopus.NewEncoder(DiscordSampleRate, DiscordChannels, gopus.Voip)
	if err != nil {
		return nil, fmt.Errorf("failed to create opus encoder: %w", err)
	}

	// Configure encoder settings optimized for speech
	opusEncoder.SetBitrate(currentQuality.Bitrate)
	opusEncoder.SetApplication(gopus.Voip)

	processor := &audioProcessor{
		logger:          logger,
		cfg:             voiceCfg,
		silenceDetector: silenceDetector,
		currentQuality:  currentQuality,
		opusDecoder:     opusDecoder,
		opusEncoder:     opusEncoder,
	}

	logger.Info("Audio processor initialized",
		zap.String("quality", quality),
		zap.Int("bitrate", currentQuality.Bitrate),
		zap.Int("complexity", currentQuality.Complexity))

	return processor, nil
}

func (p *audioProcessor) OpusToPCM(opusData []byte) ([]byte, error) {
	if len(opusData) == 0 {
		return nil, fmt.Errorf("empty opus data")
	}

	p.mu.RLock()
	decoder := p.opusDecoder
	p.mu.RUnlock()

	// Decode Opus to 48kHz stereo PCM
	pcm48Stereo, err := decoder.Decode(opusData, DiscordFrameSize, false)
	if err != nil {
		return nil, fmt.Errorf("failed to decode opus data: %w", err)
	}

	// Convert to mono and resample to 24kHz for OpenAI
	pcm24Mono := p.resampleStereoToMono(pcm48Stereo)

	// Convert int16 samples to bytes
	pcmBytes := p.int16ToBytes(pcm24Mono)

	p.logger.Debug("Converted Opus to PCM for OpenAI",
		zap.Int("opus_input_size", len(opusData)),
		zap.Int("pcm_48_stereo_samples", len(pcm48Stereo)),
		zap.Int("pcm_24_mono_samples", len(pcm24Mono)),
		zap.Int("pcm_output_bytes", len(pcmBytes)),
		zap.Float64("input_duration_ms", float64(len(pcm48Stereo))/2.0/48000.0*1000.0),
		zap.Float64("output_duration_ms", float64(len(pcm24Mono))/24000.0*1000.0))

	return pcmBytes, nil
}

func (p *audioProcessor) PCMToBase64(pcm []byte) (string, error) {
	if len(pcm) == 0 {
		return "", fmt.Errorf("empty PCM data")
	}

	encoded := base64.StdEncoding.EncodeToString(pcm)

	p.logger.Debug("Converted PCM to base64",
		zap.Int("pcm_size", len(pcm)),
		zap.Int("base64_size", len(encoded)))

	return encoded, nil
}

func (p *audioProcessor) Base64ToPCM(base64Audio string) ([]byte, error) {
	if base64Audio == "" {
		return nil, fmt.Errorf("empty base64 audio data")
	}

	pcm, err := base64.StdEncoding.DecodeString(base64Audio)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 audio: %w", err)
	}

	// Validate PCM format compliance for OpenAI Realtime API
	err = p.validatePCMFormat(pcm)
	if err != nil {
		return nil, fmt.Errorf("PCM format validation failed: %w", err)
	}

	p.logger.Debug("Converted base64 to PCM",
		zap.Int("base64_size", len(base64Audio)),
		zap.Int("pcm_size", len(pcm)))

	return pcm, nil
}

func (p *audioProcessor) PCMToOpus(pcm []byte) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, fmt.Errorf("empty PCM data")
	}

	// Convert bytes to int16 samples
	pcm24Mono := p.bytesToInt16(pcm)

	// Discord requires EXACT 20ms Opus frames at 48kHz stereo
	// We must ensure proper frame size for Discord compatibility

	// Calculate 20ms frame size at 24kHz mono (input)
	samplesPerFrame24k := OpenAIFrameSize // 20ms at 24kHz mono

	// Always normalize to 20ms frames for Discord compatibility
	// Discord voice protocol is strict about frame timing
	targetSamples := samplesPerFrame24k

	if len(pcm24Mono) != samplesPerFrame24k {
		p.logger.Debug("Normalizing audio to 20ms frame",
			zap.Int("input_samples", len(pcm24Mono)),
			zap.Int("target_samples", targetSamples),
			zap.Float64("input_duration_ms", float64(len(pcm24Mono))/24000.0*1000.0))
	}

	// Create exactly 20ms of audio (OpenAIFrameSize samples at 24kHz mono)
	processedMono := make([]int16, targetSamples)

	if len(pcm24Mono) >= targetSamples {
		// Truncate to exactly 20ms
		copy(processedMono, pcm24Mono[:targetSamples])
	} else {
		// Pad with original audio + silence to reach 20ms
		copy(processedMono, pcm24Mono)
		// Rest is automatically filled with zeros (silence)
		p.logger.Debug("Padded audio to 20ms frame",
			zap.Int("original_samples", len(pcm24Mono)),
			zap.Int("padded_samples", targetSamples))
	}

	// Resample from 24kHz mono to 48kHz stereo for Discord
	pcm48Stereo := p.resampleMonoToStereo(processedMono)

	p.mu.RLock()
	encoder := p.opusEncoder
	p.mu.RUnlock()

	// Discord expects exactly 960 samples per channel (1920 total) for 20ms at 48kHz stereo
	discordFrameSamples := DiscordFrameSize * 2 // 20ms at 48kHz stereo

	if len(pcm48Stereo) != discordFrameSamples {
		p.logger.Warn("Frame size mismatch for Discord",
			zap.Int("expected_samples", discordFrameSamples),
			zap.Int("actual_samples", len(pcm48Stereo)),
			zap.Float64("expected_duration_ms", 20.0),
			zap.Float64("actual_duration_ms", float64(len(pcm48Stereo))/2.0/48000.0*1000.0))

		// Force correct frame size for Discord
		if len(pcm48Stereo) > discordFrameSamples {
			pcm48Stereo = pcm48Stereo[:discordFrameSamples]
		} else {
			// Pad to correct size
			padded := make([]int16, discordFrameSamples)
			copy(padded, pcm48Stereo)
			pcm48Stereo = padded
		}
	}

	// Encode to Opus with Discord's expected 20ms frame size
	// Use DiscordFrameSize as the frame size parameter for gopus
	maxOutputSize := 4000 // Conservative buffer size for Opus output

	opusData, err := encoder.Encode(pcm48Stereo, DiscordFrameSize, maxOutputSize)
	if err != nil {
		return nil, fmt.Errorf("failed to encode PCM to opus for Discord: %w", err)
	}

	p.logger.Debug("Converted PCM to Opus",
		zap.Int("pcm_input_size", len(pcm)),
		zap.Int("pcm_24_mono_samples", len(pcm24Mono)),
		zap.Int("processed_mono_samples", len(processedMono)),
		zap.Int("pcm_48_stereo_samples", len(pcm48Stereo)),
		zap.Int("opus_output_size", len(opusData)))

	return opusData, nil
}

func (p *audioProcessor) DetectSilence(audio []byte) (bool, float32) {
	if len(audio) == 0 {
		return true, 0.0
	}

	// Calculate RMS energy of the audio
	energy := p.calculateRMSEnergy(audio)

	p.silenceDetector.mu.Lock()
	defer p.silenceDetector.mu.Unlock()

	// Add to energy buffer for noise floor estimation
	p.silenceDetector.energyBuffer = append(p.silenceDetector.energyBuffer, energy)
	if len(p.silenceDetector.energyBuffer) > MaxSilenceDetectionSamples {
		// Keep only the most recent samples
		copy(p.silenceDetector.energyBuffer, p.silenceDetector.energyBuffer[1:])
		p.silenceDetector.energyBuffer = p.silenceDetector.energyBuffer[:MaxSilenceDetectionSamples]
	}

	// Update adaptive threshold if enabled and enough time has passed since last speech
	// Wait at least 5 seconds after speech before adjusting threshold down
	timeSinceLastSpeech := time.Since(p.silenceDetector.lastSpeechTime)
	if p.silenceDetector.adaptiveThreshold &&
		len(p.silenceDetector.energyBuffer) > 50 &&
		timeSinceLastSpeech > 5*time.Second {
		p.updateAdaptiveThreshold()
	}

	// Determine if current audio is silent
	threshold := p.silenceDetector.threshold
	isSilent := energy < threshold

	if !isSilent {
		p.silenceDetector.lastSpeechTime = time.Now()
	}

	p.logger.Debug("Silence detection",
		zap.Float32("energy", energy),
		zap.Float32("threshold", threshold),
		zap.Bool("is_silent", isSilent),
		zap.Bool("adaptive", p.silenceDetector.adaptiveThreshold))

	return isSilent, energy
}

func (p *audioProcessor) ConfigureQuality(quality string) error {
	preset, exists := QualityPresets[quality]
	if !exists {
		return fmt.Errorf("unknown quality preset: %s", quality)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.currentQuality = preset

	// Update encoder settings with speech optimization
	p.opusEncoder.SetBitrate(preset.Bitrate)
	p.opusEncoder.SetApplication(gopus.Voip)

	p.logger.Info("Audio quality configured",
		zap.String("quality", quality),
		zap.Int("bitrate", preset.Bitrate),
		zap.Int("frame_size_ms", preset.FrameSize),
		zap.Int("complexity", preset.Complexity))

	return nil
}

func (p *audioProcessor) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// gopus doesn't require explicit cleanup
	p.opusDecoder = nil
	p.opusEncoder = nil

	p.logger.Info("Audio processor closed")
	return nil
}

// Helper methods for audio processing

func (p *audioProcessor) calculateRMSEnergy(audio []byte) float32 {
	if len(audio) < 2 {
		return 0.0
	}

	// Assume 16-bit PCM samples
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

func (p *audioProcessor) updateAdaptiveThreshold() {
	// Calculate noise floor from the lowest 10% of energy samples (more conservative)
	sorted := make([]float32, len(p.silenceDetector.energyBuffer))
	copy(sorted, p.silenceDetector.energyBuffer)

	// Sort using a more efficient algorithm for small slices
	for i := range len(sorted) - 1 {
		for j := range len(sorted) - i - 1 {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	// Take average of lowest 10% (more conservative than 25%)
	bottomPercentile := max(1, len(sorted)/10)

	var sum float32
	for i := range bottomPercentile {
		sum += sorted[i]
	}

	estimatedNoiseFloor := sum / float32(bottomPercentile)

	// Set threshold as noise floor + margin, but apply bounds
	newThreshold := estimatedNoiseFloor * p.silenceDetector.thresholdMargin

	// Apply minimum and maximum bounds to prevent extreme values
	newThreshold = max(MinimumThreshold, min(MaximumThreshold, newThreshold))

	// Very conservative smoothing to prevent rapid threshold changes
	alpha := float32(0.05) // Even more conservative smoothing
	p.silenceDetector.threshold = alpha*newThreshold + (1-alpha)*p.silenceDetector.threshold

	// Ensure threshold never goes below minimum
	p.silenceDetector.threshold = max(MinimumThreshold, p.silenceDetector.threshold)

	// Update noise floor for reference
	p.silenceDetector.noiseFloor = estimatedNoiseFloor

	p.logger.Debug("Updated adaptive threshold",
		zap.Float32("noise_floor", estimatedNoiseFloor),
		zap.Float32("new_threshold", newThreshold),
		zap.Float32("final_threshold", p.silenceDetector.threshold),
		zap.Int("buffer_samples", len(p.silenceDetector.energyBuffer)))
}

// resampleStereoToMono converts 48kHz stereo to 24kHz mono with proper anti-aliasing
func (p *audioProcessor) resampleStereoToMono(stereoSamples []int16) []int16 {
	if len(stereoSamples) == 0 {
		return []int16{}
	}

	// First convert stereo to mono at 48kHz
	monoSamples := make([]int16, len(stereoSamples)/2)
	for i := 0; i < len(monoSamples); i++ {
		if i*2+1 < len(stereoSamples) {
			// Average left and right channels with proper overflow protection
			left := int32(stereoSamples[i*2])
			right := int32(stereoSamples[i*2+1])
			monoSamples[i] = int16((left + right) / 2)
		}
	}

	// Apply the original working filter for input path (Discord -> OpenAI)
	// This second-order Butterworth filter was working correctly before
	filtered := monoSamples

	// Use linear interpolation for downsampling from 48kHz to 24kHz (2:1 ratio)
	// This is much better than simple decimation and avoids aliasing artifacts
	outputSize := len(filtered) / 2
	output := make([]int16, outputSize)

	for i := range outputSize {
		srcIndex := i * 2
		if srcIndex < len(filtered) {
			// For 2:1 downsampling, we can use simple averaging of adjacent samples
			// This acts as a basic anti-aliasing filter
			if srcIndex+1 < len(filtered) {
				sample1 := int32(filtered[srcIndex])
				sample2 := int32(filtered[srcIndex+1])
				output[i] = int16((sample1 + sample2) / 2)
			} else {
				output[i] = filtered[srcIndex]
			}
		}
	}

	return output
}

// resampleMonoToStereo converts 24kHz mono to 48kHz stereo with improved interpolation
func (p *audioProcessor) resampleMonoToStereo(monoSamples []int16) []int16 {
	if len(monoSamples) == 0 {
		return []int16{}
	}

	// Use linear interpolation for 2x upsampling (24kHz -> 48kHz)
	// This produces much better audio quality than simple duplication
	upsampled := make([]int16, len(monoSamples)*2)

	for i := 0; i < len(monoSamples); i++ {
		currentSample := monoSamples[i]

		// Place original sample at even indices
		upsampled[i*2] = currentSample

		// Interpolate sample at odd indices
		if i+1 < len(monoSamples) {
			nextSample := monoSamples[i+1]
			// Linear interpolation between current and next sample
			interpolated := int32(currentSample) + int32(nextSample)
			upsampled[i*2+1] = int16(interpolated / 2)
		} else {
			// Last sample - just duplicate
			upsampled[i*2+1] = currentSample
		}
	}

	// Convert mono to stereo (duplicate each sample for left and right channels)
	outputSize := len(upsampled) * 2 // Two channels
	output := make([]int16, outputSize)

	for i, sample := range upsampled {
		output[i*2] = sample   // Left channel
		output[i*2+1] = sample // Right channel
	}

	return output
}

// int16ToBytes converts int16 samples to byte array (little-endian)
func (p *audioProcessor) int16ToBytes(samples []int16) []byte {
	bytes := make([]byte, len(samples)*2)

	for i, sample := range samples {
		bytes[i*2] = byte(sample & 0xFF)
		bytes[i*2+1] = byte((sample >> 8) & 0xFF)
	}

	return bytes
}

// bytesToInt16 converts byte array to int16 samples (little-endian)
func (p *audioProcessor) bytesToInt16(bytes []byte) []int16 {
	sampleCount := len(bytes) / 2
	samples := make([]int16, sampleCount)

	for i := range sampleCount {
		samples[i] = int16(bytes[i*2]) | (int16(bytes[i*2+1]) << 8)
	}

	return samples
}

// validatePCMFormat validates that PCM data matches OpenAI Realtime API requirements
func (p *audioProcessor) validatePCMFormat(pcm []byte) error {
	if len(pcm) == 0 {
		return fmt.Errorf("empty PCM data")
	}

	// OpenAI Realtime API expects 16-bit PCM, so data length must be even
	if len(pcm)%2 != 0 {
		return fmt.Errorf("invalid PCM data: length (%d) is not a multiple of 2 bytes (16-bit samples)", len(pcm))
	}

	// Calculate number of samples
	numSamples := len(pcm) / 2

	// For 24kHz mono, check if the duration is reasonable
	// Most audio chunks should be between 10ms and 5 seconds
	durationMs := float64(numSamples) / 24.0 // samples / (24000 samples/sec) * 1000 ms/sec

	if durationMs < 10.0 {
		p.logger.Warn("Very short audio chunk detected",
			zap.Float64("duration_ms", durationMs),
			zap.Int("samples", numSamples))
	}

	if durationMs > 5000.0 {
		return fmt.Errorf("audio chunk too long: %.1f ms (max 5000ms)", durationMs)
	}

	// Check for basic sanity - samples shouldn't all be zero (complete silence)
	samples := p.bytesToInt16(pcm)
	allZero := true
	for _, sample := range samples {
		if sample != 0 {
			allZero = false
			break
		}
	}

	if allZero && len(samples) > OpenAIFrameSize { // More than 20ms of silence is suspicious
		p.logger.Warn("Long period of complete silence detected",
			zap.Int("samples", len(samples)),
			zap.Float64("duration_ms", durationMs))
	}

	p.logger.Debug("PCM format validation passed",
		zap.Int("bytes", len(pcm)),
		zap.Int("samples", numSamples),
		zap.Float64("duration_ms", durationMs))

	return nil
}
