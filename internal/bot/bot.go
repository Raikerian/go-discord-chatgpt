package bot

import (
	"context" // Added context
	"fmt"

	// "log" // Replaced with zap

	"github.com/Raikerian/go-discord-chatgpt/internal/commands"
	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/diamondburned/arikawa/v3/discord" // Ensure discord is imported for discord.AppID
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"go.uber.org/fx"  // Added fx import
	"go.uber.org/zap" // Added zap
)

// Bot represents the Discord bot.
type Bot struct {
	Session    *session.Session
	Config     *config.Config
	CmdManager *commands.CommandManager
	Logger     *zap.Logger // Added Logger
}

// NewBotParameters holds dependencies for NewBot
type NewBotParameters struct {
	fx.In // Required by Fx

	Cfg        *config.Config
	S          *session.Session
	Logger     *zap.Logger              // Added Logger
	CmdManager *commands.CommandManager // Added CommandManager
}

// NewBot creates and initializes a new Bot.
// The session and logger are now injected by Fx.
func NewBot(params NewBotParameters) (*Bot, error) { // Updated signature
	if params.S == nil {
		return nil, fmt.Errorf("session provided to NewBot is nil")
	}
	if params.Cfg == nil {
		return nil, fmt.Errorf("config provided to NewBot is nil")
	}
	if params.Logger == nil {
		return nil, fmt.Errorf("logger provided to NewBot is nil")
	}
	if params.CmdManager == nil { // Added check for CmdManager
		return nil, fmt.Errorf("command manager provided to NewBot is nil")
	}
	if params.Cfg.Discord.ApplicationID == nil || *params.Cfg.Discord.ApplicationID == 0 {
		return nil, fmt.Errorf("application ID is not set or is zero in config")
	}

	b := &Bot{
		Session:    params.S,
		Config:     params.Cfg,
		Logger:     params.Logger,     // Store the logger
		CmdManager: params.CmdManager, // Store the command manager
	}

	params.Logger.Info("NewBot created successfully, handler registration deferred to Start method")
	return b, nil
}

// Start now focuses on bot-specific startup logic, like registering commands
// and setting up event handlers with the correct application context.
// Session opening is handled by Fx lifecycle.
func (b *Bot) Start(ctx context.Context) error { // Added context parameter
	b.Logger.Info("Bot.Start called, application context will be used directly by handlers.")

	// Add event handlers now that ctx (the main application context) is available.
	// The closure for handleInteraction will capture the ctx from this Start method.
	b.Session.AddHandler(func(e *gateway.InteractionCreateEvent) {
		// Pass the logger and other dependencies to the handler, using ctx directly.
		handleInteraction(ctx, b.Session, e, b.Logger, b.CmdManager)
	})
	b.Logger.Info("InteractionCreateEvent handler added to session.")

	b.Logger.Info("Executing bot-specific Start logic (e.g., registering commands)...")

	// Register slash commands
	// Ensure CmdManager is initialized
	if b.CmdManager == nil {
		b.Logger.Error("Command manager is not initialized in Bot")
		return fmt.Errorf("command manager is not initialized in Bot")
	}

	// Convert string guild IDs from config to discord.GuildID
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

	b.Logger.Info("Bot started and commands registered.")

	return nil
}

// Stop gracefully shuts down the bot.
func (b *Bot) Stop(ctx context.Context) error { // Added context parameter
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
