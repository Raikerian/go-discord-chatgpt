package audio

import (
	"fmt"
)

func stereoToMono(st []int16) []int16 {
	n := len(st) / 2
	dst := make([]int16, n)
	for i := 0; i < n; i++ {
		dst[i] = int16((int32(st[2*i]) + int32(st[2*i+1])) / 2)
	}
	return dst
}

func monoToStereo(m []int16) []int16 {
	dst := make([]int16, len(m)*2)
	for i, v := range m {
		dst[2*i], dst[2*i+1] = v, v
	}
	return dst
}

func validateOpenAIPCM(pcm []byte) error {
	if len(pcm) != OpenAIFrameBytes {
		return fmt.Errorf("invalid frame length %d, want %d", len(pcm), OpenAIFrameBytes)
	}
	return nil
}
