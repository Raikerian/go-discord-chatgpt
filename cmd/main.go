package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Raikerian/go-discord-chatgpt/internal/bot"
	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"go.uber.org/fx"

	_ "github.com/Raikerian/go-discord-chatgpt/internal/commands" // Import for side effect of registering commands
)

// NewSessionParameters holds dependencies for NewSession
type NewSessionParameters struct {
	fx.In
	Cfg *config.Config
	LC  fx.Lifecycle
}

// NewSessionResult holds results from NewSession
type NewSessionResult struct {
	fx.Out
	Session *session.Session
}

// NewSession creates and manages a new Discord session.
// It provides the *session.Session to the Fx dependency graph.
// The session's Open and Close methods are tied to the Fx lifecycle.
func NewSession(params NewSessionParameters) (NewSessionResult, error) {
	if params.Cfg.BotToken == "" {
		return NewSessionResult{}, fmt.Errorf("discord bot token is not set in config")
	}

	if params.Cfg.ApplicationID == nil {
		return NewSessionResult{}, fmt.Errorf("application ID is not set in config")
	}

	s := session.New("Bot " + params.Cfg.BotToken) // Corrected: session.New returns only one value

	s.AddIntents(gateway.IntentGuilds)
	s.AddIntents(gateway.IntentGuildMessages)

	params.LC.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Println("Opening Discord session...")
			// session.Open() now takes a context
			if err := s.Open(ctx); err != nil {
				return fmt.Errorf("failed to open discord session: %w", err)
			}
			log.Println("Discord session opened successfully.")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Println("Closing Discord session...")
			if err := s.Close(); err != nil {
				log.Printf("Error closing discord session: %v", err)
				// It's often better to return the error to let Fx know shutdown didn't complete cleanly.
				return fmt.Errorf("failed to close discord session: %w", err)
			}
			log.Println("Discord session closed successfully.")
			return nil
		},
	})

	return NewSessionResult{Session: s}, nil
}

// BotLifecycleParameters holds dependencies for bot lifecycle management.
type BotLifecycleParameters struct {
	fx.In
	LC  fx.Lifecycle
	Bot *bot.Bot
}

// registerBotLifecycle hooks the bot's Start and Stop methods into the Fx application lifecycle.
func registerBotLifecycle(params BotLifecycleParameters) {
	params.LC.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Println("Starting bot...")
			if err := params.Bot.Start(); err != nil { // Assumes Bot.Start() doesn't need context
				return fmt.Errorf("failed to start bot: %w", err)
			}
			log.Println("Bot started successfully.")
			return nil
		},
		OnStop: func(ctx context.Context) error { // Assumes Bot.Stop() doesn't need context
			log.Println("Stopping bot...")
			if err := params.Bot.Stop(); err != nil {
				log.Printf("Error stopping bot: %v", err)
				return fmt.Errorf("error stopping bot: %w", err)
			}
			log.Println("Bot stopped successfully.")
			return nil
		},
	})
}

func main() {
	log.Println("Initializing Fx application...")
	app := fx.New(
		fx.Supply("../config.yaml"),
		fx.Provide(
			config.LoadConfig,
			NewSession,
			bot.NewBot,
		),
		fx.Invoke(registerBotLifecycle),
		// To see Fx's own logs, you can provide a logger.
		// For production, you might use a structured logger like zap.
		// fx.Logger(fx.Printer(log.New(os.Stdout, "[Fx] ", log.LstdFlags))),
	)

	// Start the application. This call is non-blocking.
	// It executes OnStart hooks.
	if err := app.Start(context.Background()); err != nil {
		log.Fatalf("Fx application failed to start: %v", err)
	}

	log.Println("Fx application started. Bot is running. Press CTRL-C to exit.")

	// Wait for a termination signal (CTRL-C or SIGTERM)
	// This keeps main() alive until the OS signals to shut down.
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	<-stopChan

	log.Println("Shutdown signal received. Stopping Fx application...")

	// Stop the application. This call is blocking.
	// It executes OnStop hooks.
	if err := app.Stop(context.Background()); err != nil {
		log.Fatalf("Fx application failed to stop gracefully: %v", err)
	}

	log.Println("Fx application has shut down successfully.")
}
