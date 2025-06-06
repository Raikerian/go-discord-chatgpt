package audio

import (
	"fmt"
	"sync"
)

// AudioMixer aligns mono 48-kHz frames from many SSRCs by RTP timestamp
// and accumulates mixed audio for flexible retrieval.
type AudioMixer interface {
	// AddFrame stores a single decoded PCM frame.
	// • ssrc  – Discord SSRC
	// • ts    – RTP timestamp (48-kHz clock, +960 per frame)
	// • pcm   – mono PCM, len == 960
	// Safe for concurrent use.
	AddFrame(ssrc, ts uint32, pcm []int16) error

	// GetMixed returns all currently mixed audio without modifying state.
	// Returns mono PCM samples at 48-kHz.
	GetMixed() []int16

	// Drain returns all mixed audio and clears the mixer state.
	// Returns mono PCM samples at 48-kHz, then empties the mixer.
	Drain() []int16

	// Clear discards all audio data without returning it.
	// Immediately resets mixer to initial state.
	Clear()

	// Len returns the number of mixed samples currently buffered.
	Len() int
}

// --------------------------- implementation ---------------------------

// TODO: make this configurable
const samplesPerFrame = DiscordFrameSize // 20 ms of 48 kHz mono PCM

// streamState keeps mapping between a Discord SSRC RTP clock and our shared
// mix timeline (index expressed in 20-ms frames).
//
// baseTS     – RTP timestamp of the very first packet we ever saw for this SSRC
// startFrame – global frame index that baseTS maps to (frame at which the
//
//	stream's first packet should be inserted in the final mix)
//
// lastFrame  – highest global frame index we have already inserted for this
//
//	stream (used to detect very late/out-of-order packets)
//
// All maths are done with uint32 differences so that wrap-around is handled
// automatically (RFC 3550 §5.1).
type streamState struct {
	baseTS     uint32
	startFrame int64
	lastFrame  int64
}

// mixer is a thread-safe implementation of AudioMixer.
type mixer struct {
	mu sync.Mutex

	// Per-SSRC timing information
	streams map[uint32]*streamState

	// Mixed audio buffer in *samples* (int32 to avoid overflow during summing).
	// Length == nFrames * samplesPerFrame.
	buffer []int32
}

// NewAudioMixer creates a new AudioMixer implementation.
func NewAudioMixer() AudioMixer {
	return &mixer{
		streams: make(map[uint32]*streamState),
	}
}

// AddFrame inserts one 20-ms PCM frame into the mix, automatically aligning it
// on the global timeline derived from RTP timestamps.
func (m *mixer) AddFrame(ssrc, ts uint32, pcm []int16) error {
	if len(pcm) != samplesPerFrame {
		return fmt.Errorf("pcm length must be %d, got %d", samplesPerFrame, len(pcm))
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Initialise stream state if this is the first packet for the SSRC.
	st, ok := m.streams[ssrc]
	if !ok {
		st = &streamState{
			baseTS:     ts,
			startFrame: int64(len(m.buffer)) / samplesPerFrame, // current end of mix
			lastFrame:  -1,
		}
		m.streams[ssrc] = st
	}

	// 2. Calculate this packet's frame index on the shared timeline.
	delta := uint64(ts - st.baseTS) // uint32 subtraction handles wrap-around
	frameInStream := int64(delta / samplesPerFrame)
	globalFrame := st.startFrame + frameInStream

	// Discard badly out-of-order frames that we have already mixed past.
	if globalFrame < st.lastFrame {
		return nil // silently drop late packet
	}

	st.lastFrame = globalFrame

	// 3. Grow the mixed buffer if necessary (zero-filled so it represents
	//    silence).
	neededSamples := (globalFrame + 1) * samplesPerFrame
	if int(neededSamples) > len(m.buffer) {
		m.buffer = append(m.buffer, make([]int32, int(neededSamples)-len(m.buffer))...)
	}

	// 4. Mix samples (simple sum with saturation deferred until retrieval).
	offset := globalFrame * samplesPerFrame
	for i := 0; i < samplesPerFrame; i++ {
		m.buffer[int(offset)+i] += int32(pcm[i])
	}

	return nil
}

// GetMixed returns the currently accumulated mix without clearing it.
func (m *mixer) GetMixed() []int16 {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.copyBuffer()
}

// Drain returns the mix and resets the internal state (keeping stream timing
// so that future frames continue seamlessly).
func (m *mixer) Drain() []int16 {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := m.copyBuffer()

	// Reset everything to initial state so memory doesn't grow unbounded and
	// new speakers anchor themselves relative to a fresh timeline.
	m.buffer = nil
	m.streams = make(map[uint32]*streamState)
	return out
}

// Clear discards all buffered audio *and* resets stream timing information.
func (m *mixer) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.buffer = nil
	m.streams = make(map[uint32]*streamState)
}

// Len returns the number of mixed samples currently buffered.
func (m *mixer) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.buffer)
}

/* --------------------------- helpers --------------------------- */

// copyBuffer converts the int32 accumulator to int16 with simple saturation and
// returns a *new* slice so callers can safely modify it.
func (m *mixer) copyBuffer() []int16 {
	mixed := make([]int16, len(m.buffer))
	for i, v := range m.buffer {
		mixed[i] = saturateInt16(v)
	}
	return mixed
}

// saturateInt16 clamps v to the valid int16 range.
func saturateInt16(v int32) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}
