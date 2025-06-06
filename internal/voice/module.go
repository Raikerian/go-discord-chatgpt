package voice

import (
	"github.com/Raikerian/go-discord-chatgpt/pkg/audio"
	"go.uber.org/fx"
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
