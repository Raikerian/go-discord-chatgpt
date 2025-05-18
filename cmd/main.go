package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Raikerian/go-discord-chatgpt/internal/bot"
	"github.com/Raikerian/go-discord-chatgpt/internal/config"

	_ "github.com/Raikerian/go-discord-chatgpt/internal/commands" // Import for side effect of registering commands
)

func main() {
	log.Println("Starting Discord Bot...")

	// Load configuration
	cfg, err := config.LoadConfig("../config.yaml") // Changed to look in parent directory
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Token == "" {
		log.Fatalf("Discord token is not set in config.yaml. Please update it.")
	}
	if cfg.ApplicationID == nil {
		log.Fatalf("Application ID is not set in config.yaml. Please update it.")
	}

	// Create and start the bot
	discordBot, err := bot.NewBot(cfg)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	if err := discordBot.Start(); err != nil {
		log.Fatalf("Failed to start bot: %v", err)
	}

	log.Println("Bot is now running. Press CTRL-C to exit.")

	// Wait for a termination signal
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	log.Println("Shutting down bot...")
	if err := discordBot.Stop(); err != nil {
		log.Printf("Error stopping bot: %v", err)
	}
	log.Println("Bot shut down gracefully.")
}
