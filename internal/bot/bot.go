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
	// Add other necessary fields, like a command handler
}

// NewBotParameters holds dependencies for NewBot
type NewBotParameters struct {
	fx.In // Required by Fx

	Cfg    *config.Config
	S      *session.Session
	Logger *zap.Logger // Added Logger
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
	if params.Cfg.ApplicationID == nil || *params.Cfg.ApplicationID == 0 {
		return nil, fmt.Errorf("application ID is not set or is zero in config")
	}

	b := &Bot{
		Session: params.S,
		Config:  params.Cfg,
		Logger:  params.Logger, // Store the logger
	}

	// Add handlers, such as for messages or slash commands
	params.S.AddHandler(func(e *gateway.InteractionCreateEvent) {
		// Pass the logger to the handler
		handleInteraction(context.Background(), params.S, e, params.Logger) // Corrected: Pass context and logger
	})

	// Initialize the command manager
	// Convert *discord.Snowflake to discord.AppID for NewCommandManager
	b.CmdManager = commands.NewCommandManager(params.S, discord.AppID(*params.Cfg.ApplicationID), params.Logger) // Corrected type conversion

	params.Logger.Info("NewBot created successfully")
	return b, nil
}

// Start now focuses on bot-specific startup logic, like registering commands.
// Session opening is handled by Fx lifecycle.
func (b *Bot) Start() error {
	b.Logger.Info("Executing bot-specific Start logic (e.g., registering commands)...") // Replaced log with b.Logger

	// Register slash commands
	// Ensure CmdManager is initialized
	if b.CmdManager == nil {
		b.Logger.Error("Command manager is not initialized in Bot")
		return fmt.Errorf("command manager is not initialized in Bot")
	}

	// Convert string guild IDs from config to discord.GuildID
	var guildIDs []discord.GuildID
	if b.Config != nil && len(b.Config.GuildIDs) > 0 {
		for _, idStr := range b.Config.GuildIDs {
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

	b.CmdManager.RegisterCommands(guildIDs)
	b.Logger.Info("Slash commands registration process initiated for specified guilds.") // Replaced log with b.Logger

	return nil
}

// Stop now focuses on bot-specific shutdown logic.
// Session closing is handled by Fx lifecycle.
func (b *Bot) Stop() error {
	b.Logger.Info("Executing bot-specific Stop logic...") // Replaced log with b.Logger
	// Add any bot-specific cleanup here if needed.
	return nil
}

// Remove the placeholder handleInteraction if it\'s defined elsewhere (e.g. handlers.go)
/*
func handleInteraction(s *session.Session, e *gateway.InteractionCreateEvent) {
	// Corrected usage of InteractionData: e.Data is an interface, so type assert or switch
	switch data := e.Data.(type) {
	case discord.CommandInteractionData:
		log.Printf("Command interaction received: %s from user %s", data.Name, e.Member.User.Username)
	// Add cases for other interaction types like ComponentInteractionData, ModalSubmitInteractionData, etc.
	default:
		log.Printf("Unknown interaction type received from user %s", e.Member.User.Username)
	}
}
*/
