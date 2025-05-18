package bot

import (
	"context"
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
}

// NewBot creates and initializes a new Bot.
func NewBot(cfg *config.Config) (*Bot, error) {
	s := session.New("Bot " + cfg.Token)
	if s == nil {
		return nil, fmt.Errorf("failed to create session")
	}

	b := &Bot{
		Session: s,
		Config:  cfg,
	}

	// Add handlers, such as for messages or slash commands
	s.AddHandler(func(e *gateway.InteractionCreateEvent) {
		handleInteraction(s, e) // We'll define this in handlers.go
	})

	// Add intents - adjust as needed for your bot's functionality
	s.AddIntents(gateway.IntentGuilds | gateway.IntentGuildMessages | gateway.IntentGuildIntegrations)

	return b, nil
}

// Start connects the bot to Discord and starts listening for events.
func (b *Bot) Start() error {
	log.Println("Bot starting...")
	if err := b.Session.Open(context.Background()); err != nil {
		return fmt.Errorf("failed to open session: %w", err)
	}
	log.Println("Bot connected to Discord.")

	// Register slash commands
	cmdManager := commands.NewCommandManager(b.Session, *b.Config.ApplicationID)
	cmdManager.RegisterCommands()

	return nil
}

// Stop disconnects the bot from Discord.
func (b *Bot) Stop() error {
	log.Println("Bot stopping...")
	return b.Session.Close()
}
