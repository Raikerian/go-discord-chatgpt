// Package main provides the entry point for the Discord ChatGPT bot application.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Raikerian/go-discord-chatgpt/internal/bot"
	"github.com/Raikerian/go-discord-chatgpt/internal/chat"
	"github.com/Raikerian/go-discord-chatgpt/internal/commands"
	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/Raikerian/go-discord-chatgpt/internal/gpt"

	// Import voice module.
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"

	"github.com/sashabaranov/go-openai"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

// zapFxPrinter adapts a zap.SugaredLogger to fx.Printer interface.
type zapFxPrinter struct {
	logger *zap.SugaredLogger
}

// LogEvent implements fxevent.Logger.
func (p *zapFxPrinter) LogEvent(event fxevent.Event) {
	switch e := event.(type) {
	case *fxevent.OnStartExecuting:
		p.logger.Debugf("HOOK OnStart executing: %s, function: %s", e.CallerName, e.FunctionName)
	case *fxevent.OnStartExecuted:
		p.logWithOptionalError("HOOK OnStart", e.CallerName, e.FunctionName, e.Err, e.Runtime.String())
	case *fxevent.OnStopExecuting:
		p.logger.Debugf("HOOK OnStop executing: %s, function: %s", e.CallerName, e.FunctionName)
	case *fxevent.OnStopExecuted:
		p.logWithOptionalError("HOOK OnStop", e.CallerName, e.FunctionName, e.Err, e.Runtime.String())
	case *fxevent.Supplied:
		p.logSuppliedOrProvided("SUPPLY", e.TypeName, "", e.Err)
	case *fxevent.Provided:
		p.logSuppliedOrProvided("PROVIDE", "", strings.Join(e.OutputTypeNames, ", "), e.Err)
	case *fxevent.Invoking:
		p.logger.Debugf("INVOKE: %s", e.FunctionName)
	case *fxevent.Invoked:
		p.logInvokeResult(e.FunctionName, e.Err)
	case *fxevent.Stopping:
		p.logger.Infof("STOPPING: %s", e.Signal)
	case *fxevent.Stopped:
		p.logSimpleWithError("STOPPED", e.Err)
	case *fxevent.RollingBack:
		p.logger.Errorf("ROLLING BACK: %v", e.StartErr)
	case *fxevent.RolledBack:
		p.logSimpleWithError("ROLLED BACK", e.Err)
	case *fxevent.Started:
		p.logSimpleWithError("STARTED", e.Err)
	case *fxevent.LoggerInitialized:
		p.logLoggerInitialized(e.ConstructorName, e.Err)
	default:
		p.logger.Debugf("UNKNOWN Fx event: %T", event)
	}
}

func (p *zapFxPrinter) logWithOptionalError(action, caller, function string, err error, runtime string) {
	if err != nil {
		p.logger.Errorf("%s failed: %s, function: %s, error: %v", action, caller, function, err)
	} else {
		p.logger.Debugf("%s executed: %s, function: %s, runtime: %s", action, caller, function, runtime)
	}
}

func (p *zapFxPrinter) logSuppliedOrProvided(action, typeName, outputTypes string, err error) {
	switch {
	case err != nil:
		p.logger.Errorf("%s failed: type: %s, error: %v", action, typeName, err)
	case typeName != "":
		p.logger.Debugf("%s: %s", action, typeName)
	default:
		p.logger.Debugf("%s: %s", action, outputTypes)
	}
}

func (p *zapFxPrinter) logInvokeResult(functionName string, err error) {
	if err != nil {
		p.logger.Errorf("INVOKE failed: %s, error: %v", functionName, err)
	} else {
		p.logger.Debugf("INVOKE successful: %s", functionName)
	}
}

func (p *zapFxPrinter) logSimpleWithError(action string, err error) {
	if err != nil {
		p.logger.Errorf("%s with error: %v", action, err)
	} else {
		p.logger.Info(action)
	}
}

func (p *zapFxPrinter) logLoggerInitialized(constructorName string, err error) {
	if err != nil {
		p.logger.Errorf("LOGGER INITIALIZED with error: %v", err)
	} else {
		p.logger.Debugf("LOGGER INITIALIZED: %s", constructorName)
	}
}

// Printf implements fx.Printer.
func (p *zapFxPrinter) Printf(format string, args ...interface{}) {
	// Fx's own messages. Info is usually fine.
	p.logger.Infof(format, args...)
}

// NewZapLoggerParameters holds dependencies for NewZapLogger.
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

// NewOpenAIClient creates and configures a new OpenAI client.
func NewOpenAIClient(cfg *config.Config, logger *zap.Logger) (*openai.Client, error) {
	if cfg.OpenAI.APIKey == "" {
		// It's better to return an error if the API key is missing,
		// allowing Fx to handle the startup failure gracefully.
		logger.Error("OpenAI API key is not configured in config.yaml")

		return nil, errors.New("OpenAI API key (config.OpenAI.APIKey) is not configured")
	}
	client := openai.NewClient(cfg.OpenAI.APIKey)
	logger.Info("OpenAI client created successfully.")

	return client, nil
}

// NewSessionParameters holds dependencies for NewSession.
type NewSessionParameters struct {
	fx.In
	Cfg    *config.Config
	LC     fx.Lifecycle
	Logger *zap.Logger
}

// NewSessionResult holds results from NewSession.
type NewSessionResult struct {
	fx.Out
	Session *session.Session
}

// NewSession creates and manages a new Discord session.
// It provides the *session.Session to the Fx dependency graph.
// The session's Open and Close methods are tied to the Fx lifecycle.
func NewSession(params NewSessionParameters) (NewSessionResult, error) {
	if params.Cfg.Discord.BotToken == "" {
		return NewSessionResult{}, errors.New("discord bot token is not set in config")
	}

	if params.Cfg.Discord.ApplicationID == nil {
		return NewSessionResult{}, errors.New("application ID is not set in config")
	}

	s := session.New("Bot " + params.Cfg.Discord.BotToken)

	// Add base intents including voice states for voice functionality
	s.AddIntents(gateway.IntentGuilds)
	s.AddIntents(gateway.IntentGuildMessages)

	params.LC.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			params.Logger.Info("Opening Discord session...")
			// Add necessary intents. For slash commands, GuildMessages is often sufficient.
			// Modify as needed based on other bot functionalities.
			s.AddIntents(gateway.IntentGuilds | gateway.IntentGuildMessages | gateway.IntentGuildIntegrations)
			// It is important to add handlers before opening the session.
			// Handlers are added in NewBot, which should be fine as Fx resolves dependencies.
			return s.Open(ctx)
		},
		OnStop: func(ctx context.Context) error {
			params.Logger.Info("Closing Discord session...")

			return s.Close()
		},
	})

	return NewSessionResult{Session: s}, nil
}

// provideDiscordAppID extracts the ApplicationID from the config and provides it as a discord.AppID.
// It also logs the AppID being provided.
func provideDiscordAppID(cfg *config.Config, logger *zap.Logger) (discord.AppID, error) {
	if cfg.Discord.ApplicationID == nil || *cfg.Discord.ApplicationID == 0 {
		logger.Error("Application ID is not configured or is invalid in config")

		return 0, errors.New("application ID is not configured or is invalid")
	}
	appID := discord.AppID(*cfg.Discord.ApplicationID)
	logger.Info("Providing Discord AppID", zap.Stringer("appID", appID))

	return appID, nil
}

// resolveMessageCacheSize determines the message cache size from config.
func resolveMessageCacheSize(cfg *config.Config, logger *zap.Logger) (int, error) {
	size := cfg.OpenAI.MessageCacheSize
	if size <= 0 {
		logger.Warn("OpenAI MessageCacheSize is not configured or is invalid, defaulting to 100", zap.Int("configuredSize", size))
		size = 100 // Default to a sensible value if not configured or invalid
	}
	logger.Info("Providing OpenAI MessageCacheSize", zap.Int("size", size))

	return size, nil
}

// resolveNegativeThreadCacheSize determines the negative thread cache size from config.
func resolveNegativeThreadCacheSize(cfg *config.Config, logger *zap.Logger) (int, error) {
	size := cfg.OpenAI.NegativeThreadCacheSize
	if size <= 0 {
		logger.Warn("OpenAI NegativeThreadCacheSize is not configured or is invalid, defaulting to 1000", zap.Int("configuredSize", size))
		size = 1000 // Default to a sensible value if not configured or invalid
	}
	logger.Info("Providing OpenAI NegativeThreadCacheSize", zap.Int("size", size))

	return size, nil
}

// newConversationStoreProvider creates a ConversationStore with config-derived cache sizes.
func newConversationStoreProvider(
	logger *zap.Logger,
	cfg *config.Config,
	summaryParser chat.SummaryParser,
) chat.ConversationStore {
	messageCacheSize := cfg.OpenAI.MessageCacheSize
	if messageCacheSize <= 0 {
		logger.Warn("OpenAI MessageCacheSize is not configured or is invalid, defaulting to 100", zap.Int("configuredSize", messageCacheSize))
		messageCacheSize = 100
	}

	negativeThreadCacheSize := cfg.OpenAI.NegativeThreadCacheSize
	if negativeThreadCacheSize <= 0 {
		logger.Warn("OpenAI NegativeThreadCacheSize is not configured or is invalid, defaulting to 1000", zap.Int("configuredSize", negativeThreadCacheSize))
		negativeThreadCacheSize = 1000
	}

	return chat.NewConversationStore(logger, messageCacheSize, negativeThreadCacheSize, summaryParser)
}

// Module exports Fx providers for the main application.
var Module = fx.Options(
	fx.Provide(
		// Configuration
		config.LoadConfig,

		// Logger
		NewZapLogger,

		// OpenAI Client
		NewOpenAIClient,

		// Message Cache Size (int)
		fx.Annotate(
			resolveMessageCacheSize,
			fx.ResultTags(`name:"messageCacheSize"`),
		),

		// Negative Thread Cache Size (int)
		fx.Annotate(
			resolveNegativeThreadCacheSize,
			fx.ResultTags(`name:"negativeThreadCacheSize"`),
		),

		// Message Cache
		gpt.NewMessagesCache,

		// Negative Thread Cache
		gpt.NewNegativeThreadCache,

		// Discord Session
		NewSession,

		// Discord AppID
		provideDiscordAppID,

		// Chat Service Components
		chat.NewDiscordInteractionManager,
		chat.NewOpenAIProvider,
		newConversationStoreProvider,
		chat.NewModelSelector,
		chat.NewSummaryParser,

		// Chat Service
		chat.NewService,

		// Command Manager
		commands.NewCommandManager,

		// Bot Service
		bot.NewBot,

		// Commands (grouped)
		fx.Annotate(
			commands.NewPingCommand,
			fx.As(new(commands.Command)),
			fx.ResultTags(`group:"commands"`),
		),
		fx.Annotate(
			commands.NewVersionCommand,
			fx.As(new(commands.Command)),
			fx.ResultTags(`group:"commands"`),
		),
		fx.Annotate(
			commands.NewChatCommand, // ChatCommand's constructor will now need chat.Service
			fx.As(new(commands.Command)),
			fx.ResultTags(`group:"commands"`),
		),
	),
	fx.Invoke(func(lc fx.Lifecycle, b *bot.Bot, logger *zap.Logger, s *session.Session, cfg *config.Config) {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				logger.Info("Executing OnStart hook: Starting bot and registering commands.")

				// Ensure session is open before trying to register commands
				// The session's OnStart (s.Open) should have completed due to Fx lifecycle order.

				// Start the bot, which includes registering commands
				if err := b.Start(ctx); err != nil {
					logger.Error("Failed to start bot", zap.Error(err))

					return err
				}
				logger.Info("Bot started successfully via Fx OnStart hook.")

				return nil
			},
			OnStop: func(ctx context.Context) error {
				logger.Info("Executing OnStop hook: Stopping bot and unregistering commands.")
				if err := b.Stop(ctx); err != nil {
					logger.Error("Failed to stop bot", zap.Error(err))

					return err
				}
				logger.Info("Bot stopped successfully via Fx OnStop hook.")

				return nil
			},
		})
	}),
)

func main() {
	// Set a default config path. This can be overridden by environment variables or flags if needed.
	configPath := "config.yaml"

	app := fx.New(
		Module, // Use the defined module
		// Provide the config path to the LoadConfig function.
		// Fx will see that LoadConfig needs a string and this will be used.
		fx.Supply(configPath),

		// Configure Fx to use our Zap logger for its own internal logging.
		// This makes Fx's logs consistent with the application's logs.
		fx.WithLogger(func(logger *zap.Logger) fxevent.Logger {
			// Adapt zap.Logger to fxevent.Logger. For Fx's own logs, a sugared logger is often convenient.
			return &zapFxPrinter{logger: logger.Sugar()}
		}),
	)

	// Run the application. This will block until the application stops.
	// Fx handles starting and stopping of components based on their lifecycle hooks.
	app.Run()

	// If app.Run() returns, it means the application is shutting down.
	// We can log this event. Fx has already handled the shutdown of components.

	// Set up a channel to listen for OS signals (like Ctrl+C).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Block until a signal is received.
	select {
	case s := <-sigCh:
		// Log the received signal.
		fmt.Printf("Received signal: %s, initiating shutdown.\n", s)
	case <-app.Done():
		// The application shut down for another reason (e.g., an error in a lifecycle hook).
		fmt.Println("Application shutdown initiated by Fx.")
	}

	// The Fx app's Stop method has already been called by app.Run() when it exits.
	// If you need to perform additional cleanup not managed by Fx, do it here.
	fmt.Println("Application has shut down.")
}
