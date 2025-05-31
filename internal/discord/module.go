// Package discord provides Discord-related infrastructure and Fx modules.
package discord

import (
	"context"
	"errors"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/state/store/defaultstore"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
)

// Module provides Discord-related dependencies.
var Module = fx.Module("discord",
	fx.Provide(
		NewSession,
		NewState,
		ProvideApplicationID,
	),
)

// SessionParams holds dependencies for NewSession.
type SessionParams struct {
	fx.In
	Cfg    *config.Config
	LC     fx.Lifecycle
	Logger *zap.Logger
}

// SessionResult holds results from NewSession.
type SessionResult struct {
	fx.Out
	Session *session.Session
}

// NewSession creates and manages a new Discord session.
func NewSession(params SessionParams) (SessionResult, error) {
	if params.Cfg.Discord.BotToken == "" {
		return SessionResult{}, errors.New("discord bot token is not set in config")
	}

	if params.Cfg.Discord.ApplicationID == nil {
		return SessionResult{}, errors.New("application ID is not set in config")
	}

	s := session.New("Bot " + params.Cfg.Discord.BotToken)
	s.AddIntents(gateway.IntentGuilds | gateway.IntentGuildMessages | gateway.IntentGuildIntegrations | gateway.IntentGuildVoiceStates | gateway.IntentGuildMembers)

	params.LC.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			params.Logger.Info("Opening Discord session...")

			return s.Open(ctx)
		},
		OnStop: func(ctx context.Context) error {
			params.Logger.Info("Closing Discord session...")

			return s.Close()
		},
	})

	return SessionResult{Session: s}, nil
}

// StateParams holds dependencies for NewState.
type StateParams struct {
	fx.In
	Session *session.Session
	Logger  *zap.Logger
}

// StateResult holds results from NewState.
type StateResult struct {
	fx.Out
	State *state.State
}

// NewState creates a State wrapper around the Session.
func NewState(params StateParams) StateResult {
	// Create a new cabinet with default store implementations
	cabinet := defaultstore.New()

	// Create state from existing session with proper cabinet
	st := state.NewFromSession(params.Session, cabinet)

	params.Logger.Info("Created Discord state from session with default stores")

	return StateResult{State: st}
}

// ProvideApplicationID extracts the ApplicationID from config.
func ProvideApplicationID(cfg *config.Config, logger *zap.Logger) (discord.AppID, error) {
	if cfg.Discord.ApplicationID == nil || *cfg.Discord.ApplicationID == 0 {
		logger.Error("Application ID is not configured or is invalid in config")

		return 0, errors.New("application ID is not configured or is invalid")
	}

	appID := discord.AppID(*cfg.Discord.ApplicationID)
	logger.Info("Providing Discord AppID", zap.Stringer("appID", appID))

	return appID, nil
}
