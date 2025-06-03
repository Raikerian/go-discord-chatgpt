package audiomixer

import (
	"time"
)

type AudioMixer interface {
	// Add user audio to buffer with RTP timing info
	AddUserAudioWithRTP(bufferID uint64, audio []byte, rtpTimestamp uint32, sequence uint16) error

	// Get mixed audio for a time window
	GetMixedAudio(duration time.Duration) ([]byte, error)

	// Get all currently available mixed audio based on actual RTP timestamps
	// Returns the mixed audio and the actual duration it represents
	GetAllAvailableMixedAudio() ([]byte, time.Duration, error)

	// Get all available mixed audio and immediately flush all buffers
	// This is an atomic operation that ensures buffers are cleared
	GetAllAvailableMixedAudioAndFlush() ([]byte, time.Duration, error)

	// Clear specific buffer
	ClearBuffer(bufferID uint64)

	// Clear all buffers
	ClearAllBuffers()
}
