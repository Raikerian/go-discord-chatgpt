package voice

import (
	"fmt"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
)

// Audio processing timing constants
const (
	// Audio processing timing
	DefaultFrameDuration     = 20 * time.Millisecond // 20ms frames
	DefaultSilenceThreshold  = 0.01                  // Energy threshold
	DefaultSilenceDuration   = 1500 * time.Millisecond
	DefaultInactivityTimeout = 120 * time.Second // 2 minutes
	DefaultMaxSessionLength  = 10 * time.Minute  // 10 minutes

	// Performance targets
	MaxMixingTime     = 10 * time.Millisecond // Target mixing completion time
	FallbackThreshold = 8 * time.Millisecond  // Switch to fallback mode if exceeded

	// Buffer sizes
	AudioBufferSize = 100 // Number of audio packets to buffer
	UserBufferSize  = 10  // Number of audio chunks per user
)

// Default configuration values
var (
	DefaultVoiceProfile       = "shimmer"
	DefaultAudioQuality      = "medium"
	DefaultVADMode           = "client_vad"
	DefaultMaxConcurrentSessions = 10
	DefaultShowCostWarnings  = true
	DefaultTrackSessionCosts = true
	DefaultMaxCostPerSession = 5.0
)

// Audio quality presets
var DefaultQualityPresets = map[string]AudioConfig{
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

// GetDefaultVoiceConfig returns a voice configuration with sensible defaults
func GetDefaultVoiceConfig() *config.VoiceConfig {
	return &config.VoiceConfig{
		DefaultModel:          "gpt-4o-mini-realtime-preview",
		AllowedModels:         []string{"gpt-4o-mini-realtime-preview", "gpt-4o-realtime-preview"},
		VoiceProfile:          DefaultVoiceProfile,
		AudioQuality:          DefaultAudioQuality,
		SampleRate:            OpenAISampleRate,
		SilenceThreshold:      DefaultSilenceThreshold,
		SilenceDuration:       int(DefaultSilenceDuration.Milliseconds()),
		InactivityTimeout:     int(DefaultInactivityTimeout.Seconds()),
		MaxSessionLength:      int(DefaultMaxSessionLength.Minutes()),
		MaxConcurrentSessions: DefaultMaxConcurrentSessions,
		ShowCostWarnings:      DefaultShowCostWarnings,
		TrackSessionCosts:     DefaultTrackSessionCosts,
		MaxCostPerSession:     DefaultMaxCostPerSession,
		VADMode:               DefaultVADMode,
		TurnDetection:         false,
	}
}

// ValidateVoiceConfig validates and applies defaults to a voice configuration
func ValidateVoiceConfig(cfg *config.VoiceConfig) {
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "gpt-4o-mini-realtime-preview"
	}
	
	if cfg.VoiceProfile == "" {
		cfg.VoiceProfile = DefaultVoiceProfile
	}
	
	if cfg.AudioQuality == "" {
		cfg.AudioQuality = DefaultAudioQuality
	}
	
	if cfg.SampleRate == 0 {
		cfg.SampleRate = OpenAISampleRate
	}
	
	if cfg.SilenceThreshold == 0 {
		cfg.SilenceThreshold = DefaultSilenceThreshold
	}
	
	if cfg.SilenceDuration == 0 {
		cfg.SilenceDuration = int(DefaultSilenceDuration.Milliseconds())
	}
	
	if cfg.InactivityTimeout == 0 {
		cfg.InactivityTimeout = int(DefaultInactivityTimeout.Seconds())
	}
	
	if cfg.MaxSessionLength == 0 {
		cfg.MaxSessionLength = int(DefaultMaxSessionLength.Minutes())
	}
	
	if cfg.MaxConcurrentSessions == 0 {
		cfg.MaxConcurrentSessions = DefaultMaxConcurrentSessions
	}
	
	if cfg.MaxCostPerSession == 0 {
		cfg.MaxCostPerSession = DefaultMaxCostPerSession
	}
	
	if cfg.VADMode == "" {
		cfg.VADMode = DefaultVADMode
	}
}

// FormatDuration formats a duration for display
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	
	if minutes < 60 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	
	hours := minutes / 60
	minutes = minutes % 60
	
	return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
}

// FormatCost formats a cost value for display
func FormatCost(cost float64) string {
	if cost < 0.01 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", cost)
}