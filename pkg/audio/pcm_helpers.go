package audio

import (
	"bytes"
	"encoding/binary"
)

// PCMInt16ToLE converts int16 samples to raw little-endian bytes.
func PCMInt16ToLE(samples []int16) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, samples)
	return buf.Bytes()
}

// LEToPCMInt16 converts raw little-endian bytes back to int16 samples.
func LEToPCMInt16(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	_ = binary.Read(bytes.NewReader(b), binary.LittleEndian, &out)
	return out
}
