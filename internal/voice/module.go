package voice

import (
	"go.uber.org/fx"

	"github.com/Raikerian/go-discord-chatgpt/pkg/audio"
)

var Module = fx.Module("voice",
	fx.Provide(
		NewDiscordVoiceManager,
		audio.NewAudioProcessor,
		NewRealtimeProvider,
		NewSessionManager,
		audio.NewAudioMixer,
		NewService,
	),
)
