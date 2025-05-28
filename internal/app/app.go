// Package app provides the main application structure and lifecycle management.
package app

import (
	"context"

	"github.com/diamondburned/arikawa/v3/session"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/bot"
)

// Application represents the main application with its lifecycle.
type Application struct {
	app *fx.App
}

// New creates a new Application with the provided modules and options.
func New(modules ...fx.Option) *Application {
	// Combine all provided modules with lifecycle management
	options := append(modules, fx.Invoke(registerLifecycleHooks))

	app := fx.New(options...)

	return &Application{
		app: app,
	}
}

// Run starts the application and blocks until it's stopped.
func (a *Application) Run() {
	a.app.Run()
}

// Stop gracefully stops the application.
func (a *Application) Stop(ctx context.Context) error {
	return a.app.Stop(ctx)
}

// registerLifecycleHooks sets up the application lifecycle hooks.
func registerLifecycleHooks(lc fx.Lifecycle, b *bot.Bot, logger *zap.Logger, s *session.Session) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("Starting application: Starting bot and registering commands")

			// Start the bot, which includes registering commands
			if err := b.Start(ctx); err != nil {
				logger.Error("Failed to start bot", zap.Error(err))

				return err
			}

			logger.Info("Application started successfully")

			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("Stopping application: Stopping bot and unregistering commands")

			if err := b.Stop(ctx); err != nil {
				logger.Error("Failed to stop bot", zap.Error(err))

				return err
			}

			logger.Info("Application stopped successfully")

			return nil
		},
	})
}
