package bot

import (
	"fmt"
	"log"

	"github.com/Raikerian/go-discord-chatgpt/internal/commands"
	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
)

// Bot represents the Discord bot.
type Bot struct {
	Session *session.Session
	Config  *config.Config
	// Add other necessary fields, like a command handler
	CmdManager *commands.CommandManager // Added to store the command manager
}

// NewBot creates and initializes a new Bot.
// The session is now injected by Fx.
func NewBot(cfg *config.Config, s *session.Session) (*Bot, error) {
	if s == nil {
		return nil, fmt.Errorf("session provided to NewBot is nil")
	}
	if cfg == nil {
		return nil, fmt.Errorf("config provided to NewBot is nil")
	}
	if cfg.ApplicationID == nil || *cfg.ApplicationID == 0 {
		return nil, fmt.Errorf("application ID is not set or is zero in config")
	}

	b := &Bot{
		Session: s,
		Config:  cfg,
	}

	// Add handlers, such as for messages or slash commands
	// It's generally good practice to add handlers here during construction.
	s.AddHandler(func(e *gateway.InteractionCreateEvent) {
		// Assuming handleInteraction is defined in handlers.go and imported correctly
		// If it's in the same package, direct call is fine.
		// If it's in a different package, it needs to be exported and imported.
		// For now, let's assume it's accessible.
		handleInteraction(s, e)
	})

	// Intents are now added in NewSession in main.go
	// s.AddIntents(gateway.IntentGuilds | gateway.IntentGuildMessages | gateway.IntentGuildIntegrations)

	// Initialize the command manager
	b.CmdManager = commands.NewCommandManager(s, *cfg.ApplicationID)

	return b, nil
}

// Start now focuses on bot-specific startup logic, like registering commands.
// Session opening is handled by Fx lifecycle.
func (b *Bot) Start() error {
	// Log that bot-specific start logic is running
	log.Println("Executing bot-specific Start logic (e.g., registering commands)...")

	// Register slash commands
	// Ensure CmdManager is initialized
	if b.CmdManager == nil {
		return fmt.Errorf("command manager is not initialized in Bot")
	}
	b.CmdManager.RegisterCommands()
	log.Println("Slash commands registered.")

	return nil
}

// Stop now focuses on bot-specific shutdown logic.
// Session closing is handled by Fx lifecycle.
func (b *Bot) Stop() error {
	// Log that bot-specific stop logic is running
	log.Println("Executing bot-specific Stop logic...")
	// Add any bot-specific cleanup here if needed.
	// For example, if there are background goroutines started by the bot
	// (not the session), they could be signaled to stop here.
	return nil
}

// Remove the placeholder handleInteraction if it's defined elsewhere (e.g. handlers.go)
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
