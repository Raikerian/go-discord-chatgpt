package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Raikerian/go-discord-chatgpt/internal/bot"
	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent" // Added fxevent
	"go.uber.org/zap"

	_ "github.com/Raikerian/go-discord-chatgpt/internal/commands" // Import for side effect of registering commands
)

// zapFxPrinter adapts a zap.SugaredLogger to fx.Printer interface
type zapFxPrinter struct {
	logger *zap.SugaredLogger
}

// Printf implements fx.Printer
func (p *zapFxPrinter) Printf(format string, args ...interface{}) {
	// You can choose the log level for Fx's own messages. Info is usually fine.
	p.logger.Infof(format, args...)
}

// NewZapLoggerParameters holds dependencies for NewZapLogger
type NewZapLoggerParameters struct {
	fx.In
	Cfg *config.Config
	LC  fx.Lifecycle
}

// NewZapLogger creates and configures a new Zap logger.
func NewZapLogger(params NewZapLoggerParameters) (*zap.Logger, error) {
	var zapConfig zap.Config
	switch params.Cfg.LogLevel {
	case "debug":
		zapConfig = zap.NewDevelopmentConfig()
	case "info":
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapConfig = zap.NewProductionConfig() // Default to info
		zapConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	logger, err := zapConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create zap logger: %w", err)
	}

	params.LC.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// Flushes any buffered log entries
			return logger.Sync()
		},
	})

	return logger, nil
}

// NewSessionParameters holds dependencies for NewSession
type NewSessionParameters struct {
	fx.In
	Cfg    *config.Config
	LC     fx.Lifecycle
	Logger *zap.Logger // Added Logger
}

// NewSessionResult holds results from NewSession
type NewSessionResult struct {
	fx.Out
	Session *session.Session
}

// NewSession creates and manages a new Discord session.
// It provides the *session.Session to the Fx dependency graph.
// The session\'s Open and Close methods are tied to the Fx lifecycle.
func NewSession(params NewSessionParameters) (NewSessionResult, error) { // Added Logger to params
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
			params.Logger.Info("Opening Discord session...") // Replaced log with params.Logger
			// session.Open() now takes a context
			if err := s.Open(ctx); err != nil {
				params.Logger.Error("Failed to open discord session", zap.Error(err)) // Replaced log with params.Logger
				return fmt.Errorf("failed to open discord session: %w", err)
			}
			params.Logger.Info("Discord session opened successfully.") // Replaced log with params.Logger
			return nil
		},
		OnStop: func(ctx context.Context) error {
			params.Logger.Info("Closing Discord session...") // Replaced log with params.Logger
			if err := s.Close(); err != nil {
				params.Logger.Error("Error closing discord session", zap.Error(err)) // Replaced log with params.Logger
				// It\'s often better to return the error to let Fx know shutdown didn\'t complete cleanly.
				return fmt.Errorf("failed to close discord session: %w", err)
			}
			params.Logger.Info("Discord session closed successfully.") // Replaced log with params.Logger
			return nil
		},
	})

	return NewSessionResult{Session: s}, nil
}

// BotLifecycleParameters holds dependencies for bot lifecycle management.
type BotLifecycleParameters struct {
	fx.In
	LC     fx.Lifecycle
	Bot    *bot.Bot
	Logger *zap.Logger // Added Logger
}

// registerBotLifecycle hooks the bot\'s Start and Stop methods into the Fx application lifecycle.
func registerBotLifecycle(params BotLifecycleParameters) { // Added Logger to params
	params.LC.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			params.Logger.Info("Starting bot...")      // Replaced log with params.Logger
			if err := params.Bot.Start(); err != nil { // Assumes Bot.Start() doesn\'t need context
				params.Logger.Error("Failed to start bot", zap.Error(err)) // Replaced log with params.Logger
				return fmt.Errorf("failed to start bot: %w", err)
			}
			params.Logger.Info("Bot started successfully.") // Replaced log with params.Logger
			return nil
		},
		OnStop: func(ctx context.Context) error { // Assumes Bot.Stop() doesn\'t need context
			params.Logger.Info("Stopping bot...") // Replaced log with params.Logger
			if err := params.Bot.Stop(); err != nil {
				params.Logger.Error("Error stopping bot", zap.Error(err)) // Replaced log with params.Logger
				return fmt.Errorf("error stopping bot: %w", err)
			}
			params.Logger.Info("Bot stopped successfully.") // Replaced log with params.Logger
			return nil
		},
	})
}

func main() {
	app := fx.New(
		fx.Supply("../config.yaml"), // Provide the config file path
		fx.Provide(
			config.LoadConfig, // Provide configuration loading function
			NewZapLogger,      // Provide our Zap logger
			NewSession,        // Provide Discord session factory
			bot.NewBot,        // Provide Bot factory
			// Provide fxevent.Logger using the existing *zap.Logger
			func(log *zap.Logger) fxevent.Logger {
				return &fxevent.ZapLogger{Logger: log}
			},
		),
		fx.Invoke(registerBotLifecycle), // Invoke functions that need to run on start (e.g., register lifecycle hooks)
		// Provide the zap logger to Fx itself
		fx.WithLogger(func(logger *zap.Logger) fx.Printer {
			return &zapFxPrinter{logger: logger.Sugar()} // Use the adapter
		}),
	)

	// Start the application.
	if err := app.Start(context.Background()); err != nil {
		// Fx's own logger (now our Zap logger) should have logged details.
		// This provides a clear message to stderr and exits.
		fmt.Fprintf(os.Stderr, "Fx application failed to start: %v\\n", err)
		os.Exit(1)
	}

	// At this point, the application is running.
	// The injected logger in `main`'s scope isn't directly available without further Fx constructs.
	// We rely on Fx's logging or component-specific logging.

	// Wait for a termination signal (CTRL-C or SIGTERM)
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	<-stopChan // Block until a signal is received

	// Stop the application.
	if err := app.Stop(context.Background()); err != nil {
		// Fx's own logger (now our Zap logger) should have logged details.
		fmt.Fprintf(os.Stderr, "Fx application failed to stop gracefully: %v\\n", err)
		os.Exit(1)
	}

	// The logger.Sync() for the main Zap logger is handled by its Fx lifecycle hook in NewZapLogger.
}
