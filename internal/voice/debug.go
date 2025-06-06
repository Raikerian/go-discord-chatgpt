// Package voice provides Discord voice channel functionality including audio streaming and real-time transcription.
package voice

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/pkg/audio"
)

// saveDebugWAV writes a mono 16-bit PCM buffer to disk as a WAV file.
// samples     – raw PCM samples BEFORE down-sampling (e.g. 48 kHz, mono)
// sampleRate  – Hz of the samples slice (48 000 for Discord frames)
// prefix      – filename prefix (e.g. "mixed" or "user123").
func (s *Service) saveDebugWAV(samples []int16, sampleRate int,
	guildID discord.GuildID, prefix string) error {
	if len(samples) == 0 {
		return errors.New("saveDebugWAV: empty sample slice")
	}

	// 1.  Prepare output directory & filename.
	const debugDir = "debug_audio"
	if err := os.MkdirAll(debugDir, 0o750); err != nil {
		return fmt.Errorf("debug dir: %w", err)
	}
	// Sanitize filename components to prevent path injection
	safePrefix := filepath.Base(prefix)
	safeGuildID := filepath.Base(guildID.String())
	filename := filepath.Join(
		debugDir,
		fmt.Sprintf("%s_audio_%s_%s.wav",
			safePrefix, safeGuildID, time.Now().Format("20060102_150405")),
	)

	// 2.  Convert int16 → little-endian bytes.
	pcmBytes := audio.PCMInt16ToLE(samples)

	// 3.  WAV header constants.
	const (
		numChannels   = 1
		bitsPerSample = 16
	)
	if sampleRate < 0 || sampleRate > 0x7FFFFFFF/numChannels/bitsPerSample*8 {
		return errors.New("sample rate too large")
	}
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	if len(pcmBytes) > 0xFFFFFFFF {
		return errors.New("PCM data too large for WAV format")
	}
	dataSize := uint32(len(pcmBytes))
	fileSize := dataSize + 36 // header = 44 bytes, RIFF size = fileSize+8-12
	// 4.  Write file.
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create wav: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			s.logger.Error("failed to close debug WAV file", zap.Error(closeErr))
		}
	}()

	write := func(v interface{}) error {
		return binary.Write(file, binary.LittleEndian, v)
	}

	// RIFF chunk
	if _, err = file.WriteString("RIFF"); err != nil {
		return err
	}
	if err = write(fileSize); err != nil {
		return err
	}
	if _, err = file.WriteString("WAVE"); err != nil {
		return err
	}

	// fmt  sub-chunk
	if _, err = file.WriteString("fmt "); err != nil {
		return err
	}
	if err = write(uint32(16)); err != nil { // PCM header size
		return err
	}
	if err = write(uint16(1)); err != nil { // PCM format
		return err
	}
	if err = write(uint16(numChannels)); err != nil {
		return err
	}
	if err = write(uint32(sampleRate)); err != nil {
		return err
	}
	if err = write(uint32(byteRate)); err != nil {
		return err
	}
	if err = write(uint16(blockAlign)); err != nil {
		return err
	}
	if err = write(uint16(bitsPerSample)); err != nil {
		return err
	}

	// data sub-chunk
	if _, err = file.WriteString("data"); err != nil {
		return err
	}
	if err = write(dataSize); err != nil {
		return err
	}
	if _, err = file.Write(pcmBytes); err != nil {
		return err
	}

	s.logger.Info("saved debug WAV",
		zap.String("file", filename),
		zap.Int("samples", len(samples)),
		zap.Int("rate_hz", sampleRate),
		zap.Float64("duration_sec",
			float64(len(samples))/float64(sampleRate)))

	return nil
}
