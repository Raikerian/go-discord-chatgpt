package audio

// Format constants shared by the codec and mixer layers.
const (
	// Discord input.
	DiscordSampleRate = 48_000 // Hz
	DiscordChannels   = 2      // interleaved stereo
	DiscordFrameSize  = 960    // samples per channel (20 ms)

	// OpenAI output.
	OpenAISampleRate = 24_000 // Hz
	OpenAIChannels   = 1
	OpenAIFrameSize  = 480                 // samples (20 ms)
	OpenAIFrameBytes = OpenAIFrameSize * 2 // 16-bit PCM
)
