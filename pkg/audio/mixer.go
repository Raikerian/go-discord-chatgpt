package audio

import (
	"context"
)

// AudioMixer aligns mono 48-kHz frames from many SSRCs by RTP timestamp
// and emits one mixed frame (960 samples, 20 ms).
type AudioMixer interface {
	// PushFrame stores a single decoded PCM frame.
	// • ssrc  – Discord SSRC
	// • ts    – RTP timestamp (48-kHz clock, +960 per frame)
	// • pcm   – mono PCM, len == 960
	// Safe for concurrent use.
	PushFrame(ssrc, ts uint32, pcm []int16) error

	// ReadMixed blocks until the next aligned mix is ready or ctx is canceled.
	// It returns the mono frame (len == 960).
	ReadMixed(ctx context.Context) (frame []int16, err error)

	// Reset clears all internal state.
	Reset()
}
