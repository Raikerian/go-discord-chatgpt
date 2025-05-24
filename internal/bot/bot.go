package bot

import (
	"context"
	"errors"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/chat"
	"github.com/Raikerian/go-discord-chatgpt/internal/commands"
	"github.com/Raikerian/go-discord-chatgpt/internal/config"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Bot represents the Discord bot.
type Bot struct {
	Session     *session.Session
	Config      *config.Config
	CmdManager  *commands.CommandManager
	Logger      *zap.Logger
	ChatService *chat.Service
}

// NewBotParameters holds dependencies for NewBot.
type NewBotParameters struct {
	fx.In // Required by Fx

	Cfg        *config.Config
	S          *session.Session
	Logger     *zap.Logger
	CmdManager *commands.CommandManager
	ChatSvc    *chat.Service
}

// NewBot creates and initializes a new Bot.
// The session, logger, and chat service are now injected by Fx.
func NewBot(params NewBotParameters) (*Bot, error) {
	if params.S == nil {
		return nil, errors.New("session provided to NewBot is nil")
	}
	if params.Cfg == nil {
		return nil, errors.New("config provided to NewBot is nil")
	}
	if params.Logger == nil {
		return nil, errors.New("logger provided to NewBot is nil")
	}
	if params.CmdManager == nil {
		return nil, errors.New("command manager provided to NewBot is nil")
	}
	if params.ChatSvc == nil {
		return nil, errors.New("chat service provided to NewBot is nil")
	}
	if params.Cfg.Discord.ApplicationID == nil || *params.Cfg.Discord.ApplicationID == 0 {
		return nil, errors.New("application ID is not set or is zero in config")
	}

	b := &Bot{
		Session:     params.S,
		Config:      params.Cfg,
		Logger:      params.Logger,
		CmdManager:  params.CmdManager,
		ChatService: params.ChatSvc, // Initialize ChatService
	}

	params.Logger.Info("NewBot created successfully. Handler registration will occur in Start.")

	return b, nil
}

// Start now focuses on bot-specific startup logic, like registering commands
// and setting up event handlers with the correct application context.
// Session opening is handled by Fx lifecycle.
func (b *Bot) Start(ctx context.Context) error {
	b.Logger.Info("Bot.Start called.")

	// Determine interaction timeout
	// Default to 30 seconds if not specified or invalid in config
	interactionTimeout := 30 * time.Second
	if b.Config != nil && b.Config.Discord.InteractionTimeoutSeconds > 0 {
		interactionTimeout = time.Duration(b.Config.Discord.InteractionTimeoutSeconds) * time.Second
		b.Logger.Info("Using interaction timeout from config", zap.Duration("timeout", interactionTimeout))
	} else {
		b.Logger.Info("Using default interaction timeout", zap.Duration("timeout", interactionTimeout))
	}

	// Add event handlers now that the bot is "starting".
	// The Fx application context (ctx) is for the Start method's lifecycle,
	// not for individual interactions.
	b.Session.AddHandler(func(e *gateway.InteractionCreateEvent) {
		// Create a new context with a timeout for each interaction.
		interactionCtx, cancel := context.WithTimeout(context.Background(), interactionTimeout)
		defer cancel()

		handleInteraction(interactionCtx, b.Session, e, b.Logger, b.CmdManager)
	})

	// Add MessageCreateEvent handler
	b.Session.AddHandler(func(e *gateway.MessageCreateEvent) {
		// Filter out messages from bots
		if e.Author.Bot {
			return
		}
		// Create a new context for each message.
		b.handleMessageCreate(context.Background(), b.Session, e)
	})
	b.Logger.Info("InteractionCreateEvent and MessageCreateEvent handlers added to session.")

	b.Logger.Info("Executing further bot-specific Start logic (e.g., registering commands)...")

	// Register slash commands
	// Ensure CmdManager is initialized
	if b.CmdManager == nil {
		b.Logger.Error("Command manager is not initialized in Bot")

		return errors.New("command manager is not initialized in Bot")
	}

	var guildIDs []discord.GuildID
	if b.Config != nil && len(b.Config.Discord.GuildIDs) > 0 {
		for _, idStr := range b.Config.Discord.GuildIDs {
			sf, err := discord.ParseSnowflake(idStr)
			if err != nil {
				b.Logger.Error("Failed to parse guild ID string to Snowflake", zap.String("guildIDStr", idStr), zap.Error(err))

				continue // Skip invalid IDs
			}
			guildIDs = append(guildIDs, discord.GuildID(sf))
		}
	} else {
		b.Logger.Warn("No GuildIDs found in config, or config is nil. Commands might not be registered to specific guilds.")
		// Depending on desired behavior, you might want to return an error here
		// or proceed with registering global commands (if that's an intended fallback).
		// For now, we'll proceed, and RegisterCommands will handle an empty slice if necessary.
	}

	// Register slash commands on startup
	b.CmdManager.RegisterCommands(guildIDs)

	b.Logger.Info("Bot started, event handler and commands registered.")

	return nil
}

// Stop gracefully shuts down the bot.
func (b *Bot) Stop(ctx context.Context) error {
	b.Logger.Info("Stopping bot...")

	// Unregister slash commands on shutdown
	var guildIDs []discord.GuildID
	if b.Config != nil && len(b.Config.Discord.GuildIDs) > 0 {
		for _, idStr := range b.Config.Discord.GuildIDs {
			sf, err := discord.ParseSnowflake(idStr)
			if err != nil {
				b.Logger.Error("Failed to parse guild ID string to Snowflake for unregistering", zap.String("guildIDStr", idStr), zap.Error(err))

				continue
			}
			guildIDs = append(guildIDs, discord.GuildID(sf))
		}
	}
	if b.CmdManager != nil {
		b.CmdManager.UnregisterAllCommands(guildIDs)
	}

	b.Logger.Info("Bot stopped.")

	return nil
}
