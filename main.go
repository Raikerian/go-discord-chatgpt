// Package main provides the entry point for the Discord ChatGPT bot application.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/app"
	"github.com/Raikerian/go-discord-chatgpt/internal/bot"
	"github.com/Raikerian/go-discord-chatgpt/internal/chat"
	"github.com/Raikerian/go-discord-chatgpt/internal/commands"
	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/Raikerian/go-discord-chatgpt/internal/discord"
	"github.com/Raikerian/go-discord-chatgpt/internal/gpt"
	"github.com/Raikerian/go-discord-chatgpt/internal/infrastructure"
	"github.com/Raikerian/go-discord-chatgpt/internal/openai"
	pkginfra "github.com/Raikerian/go-discord-chatgpt/pkg/infrastructure"

	"go.uber.org/fx"
)

func main() {
	// Set a default config path. This can be overridden by environment variables or flags if needed.
	configPath := "config.yaml"

	// Create the application with all modules
	application := app.New(
		// Core modules
		config.Module,
		infrastructure.LoggerModule,

		// External service modules
		discord.Module,
		openai.Module,

		// Application modules
		gpt.Module,
		chat.Module,
		commands.Module,
		bot.Module,

		// Supply the config path
		fx.Supply(configPath),

		// Configure Fx to use our Zap logger for its own internal logging
		fx.WithLogger(pkginfra.NewFxLoggerAdapter),
	)

	// Set up a channel to listen for OS signals (like Ctrl+C)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the application in a goroutine
	go application.Run()

	// Block until a signal is received
	sig := <-sigCh
	fmt.Printf("Received signal: %s, initiating shutdown.\n", sig)

	// Create a context with timeout for graceful shutdown
	// Give the application 30 seconds to shut down gracefully
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	// Gracefully stop the application
	err := application.Stop(shutdownCtx)
	cancel() // Always cancel the context after Stop returns

	if err != nil {
		fmt.Printf("Error during shutdown: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Application has shut down gracefully.")
}
