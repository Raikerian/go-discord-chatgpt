package voice

import (
	"go.uber.org/fx"
)

var Module = fx.Module("voice",
	fx.Provide(
		NewDiscordVoiceManager,
		NewAudioProcessor,
		NewRealtimeProvider,
		NewSessionManager,
		// audiomixer.NewAudioMixer,
		NewService,
	),
)
