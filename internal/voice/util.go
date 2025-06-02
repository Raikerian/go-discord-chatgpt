package voice

import (
	"time"
)

// Audio processing timing constants.
const (
	// Audio processing timing.
	DefaultFrameDuration     = 20 * time.Millisecond // 20ms frames
	DefaultSilenceThreshold  = 0.01                  // Energy threshold
	DefaultSilenceDuration   = 1500 * time.Millisecond
	DefaultInactivityTimeout = 120 * time.Second // 2 minutes
	DefaultMaxSessionLength  = 10 * time.Minute  // 10 minutes

	// Performance targets.
	MaxMixingTime     = 10 * time.Millisecond // Target mixing completion time
	FallbackThreshold = 8 * time.Millisecond  // Switch to fallback mode if exceeded

	// Buffer sizes.
	AudioBufferSize = 100 // Number of audio packets to buffer
	UserBufferSize  = 10  // Number of audio chunks per user

	// Timeout intervals.
	AudioTimeoutCheckInterval = 100 * time.Millisecond // How often to check for audio timeouts
)
