package audio

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sync"

	"layeh.com/gopus"
)

// AudioProcessor converts between Discord Opus and the 24-kHz mono
// PCM format expected by the OpenAI Realtime API.
//
// ───────────────────────────── pipeline ─────────────────────────────
// Opus ──▶ OpusToPCM48() ──▶ mixer(48 k) ──▶ DownsamplePCM() ──▶ b64
//
//	▲                                                       │
//	└──── PCM48MonoToOpus() ◄── UpsamplePCM() ◄─────────────┘
type AudioProcessor interface {
	// ---- Discord → mixer --------------------------------------------
	OpusToPCM48(opus []byte) ([]int16, error) // 48 k mono, 960 samples

	// ---- Resampling helpers -----------------------------------------
	DownsamplePCM(src []int16, srcRate, dstRate int) ([]int16, error) // generic
	UpsamplePCM(src []int16, srcRate, dstRate int) ([]int16, error)

	// ---- mixer result → Discord -------------------------------------
	PCM48MonoToOpus(pcm48 []int16) ([]byte, error) // expects 960 samples

	// ---- Convenience -------------------------------------------------
	PCMToBase64(pcm []byte) (string, error)
	Base64ToPCM(b64 string) ([]byte, error)
}

type audioProcessor struct {
	closed bool

	// Opus codecs
	opusDecoder *gopus.Decoder
	opusEncoder *gopus.Encoder

	// Thread safety
	mu sync.RWMutex
}

func NewAudioProcessor() (AudioProcessor, error) {
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
	opusEncoder.SetBitrate(48000)

	processor := &audioProcessor{
		opusDecoder: opusDecoder,
		opusEncoder: opusEncoder,
	}

	return processor, nil
}

func (p *audioProcessor) OpusToPCM48(opus []byte) ([]int16, error) {
	if len(opus) == 0 {
		return nil, errors.New("opus payload empty")
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return nil, errors.New("processor closed")
	}

	// 1. Decode 48 kHz stereo.
	raw, err := p.opusDecoder.Decode(opus, DiscordFrameSize, false)
	if err != nil {
		return nil, fmt.Errorf("opus decode: %w", err)
	}

	// 2. Down-mix L+R → mono.
	return stereoToMono(raw), nil
}

// PCM48MonoToOpus encodes one 20-ms mono frame (960 samples @48 k) into
// a Discord-ready Opus packet (interleaved stereo, 48 k clock).
func (p *audioProcessor) PCM48MonoToOpus(pcm48 []int16) ([]byte, error) {
	if len(pcm48) != DiscordFrameSize {
		return nil, fmt.Errorf("need 960 samples, got %d", len(pcm48))
	}

	// mono ➜ stereo duplication
	stereo := monoToStereo(pcm48)

	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return nil, errors.New("processor closed")
	}
	return p.opusEncoder.Encode(stereo, DiscordFrameSize, 0)
}

func (p *audioProcessor) DownsamplePCM(src []int16, srcRate, dstRate int) ([]int16, error) {
	if len(src) == 0 {
		return nil, errors.New("pcm empty")
	}
	if srcRate <= dstRate || srcRate%dstRate != 0 {
		return nil, fmt.Errorf("unsupported ratio %d:%d", srcRate, dstRate)
	}
	factor := srcRate / dstRate
	dst := make([]int16, 0, len(src)/factor)
	for i := 0; i < len(src); i += factor {
		dst = append(dst, src[i]) // naive decimation
	}
	return dst, nil
}

// UpsamplePCM returns a new slice whose length = len(src)*dstRate/srcRate.
// Only integer factors are supported (e.g. 24 k ➜ 48 k, factor = 2).
func (p *audioProcessor) UpsamplePCM(src []int16, srcRate, dstRate int) ([]int16, error) {
	if len(src) == 0 {
		return nil, errors.New("pcm empty")
	}
	if dstRate <= srcRate || dstRate%srcRate != 0 {
		return nil, fmt.Errorf("unsupported ratio %d:%d", srcRate, dstRate)
	}
	factor := dstRate / srcRate
	dst := make([]int16, len(src)*factor)
	for i, v := range src {
		for k := 0; k < factor; k++ {
			dst[i*factor+k] = v // zero-order hold
		}
	}
	return dst, nil
}

/* ---------------------------  Base-64 helpers  ------------------------ */

func (p *audioProcessor) PCMToBase64(pcm []byte) (string, error) {
	if len(pcm) == 0 {
		return "", errors.New("pcm empty")
	}
	return base64.StdEncoding.EncodeToString(pcm), nil
}

func (p *audioProcessor) Base64ToPCM(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, errors.New("base64 empty")
	}
	pcm, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	if err := validateOpenAIPCM(pcm); err != nil {
		return nil, err
	}
	return pcm, nil
}
