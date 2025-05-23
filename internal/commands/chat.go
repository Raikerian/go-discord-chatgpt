package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"

	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/chat" // Import the new chat service package
	"github.com/Raikerian/go-discord-chatgpt/internal/config"
)

// ChatCommand handles the /chat command logic.
// It now delegates most of its work to the chat.Service.
type ChatCommand struct {
	logger      *zap.Logger
	cfg         *config.Config // Retained for model list in Options()
	chatService *chat.Service
}

// NewChatCommand creates a new ChatCommand.
// It requires a logger, config, and the chat.Service.
func NewChatCommand(logger *zap.Logger, cfg *config.Config, chatService *chat.Service) Command {
	return &ChatCommand{
		logger:      logger.Named("chat_command"),
		cfg:         cfg,
		chatService: chatService,
	}
}

// Name returns the name of the command.
func (c *ChatCommand) Name() string {
	return "chat"
}

// Description returns the description of the command.
func (c *ChatCommand) Description() string {
	return "Starts a chat session with ChatGPT."
}

// Options returns the command options for the /chat command.
// It includes a required "message" option and an optional "model" option
// if AI models are configured.
func (c *ChatCommand) Options() []discord.CommandOption {
	baseOptions := []discord.CommandOption{
		&discord.StringOption{
			OptionName:  "message",
			Description: "Your message to ChatGPT",
			Required:    true,
		},
	}

	// Model options are still determined here based on config
	if c.cfg != nil && len(c.cfg.OpenAI.Models) > 0 {
		modelChoices := make([]discord.StringChoice, len(c.cfg.OpenAI.Models))
		for i, modelName := range c.cfg.OpenAI.Models {
			modelChoices[i] = discord.StringChoice{
				Name:  modelName,
				Value: modelName,
			}
		}

		baseOptions = append(baseOptions, &discord.StringOption{
			OptionName:  "model",
			Description: "Specific AI model to use (optional, defaults to first configured model)",
			Required:    false,
			Choices:     modelChoices,
		})
	}
	return baseOptions
}

// Execute handles the execution of the /chat command.
// It parses options and then calls the chat.Service to handle the core logic.
func (c *ChatCommand) Execute(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error {
	c.logger.Info("Chat command execution initiated",
		zap.String("user", e.Member.User.Username),
		zap.String("userID", e.Member.User.ID.String()),
	)

	// 1. Parse options
	var userPrompt, modelOption string
	for _, opt := range data.Options {
		switch opt.Name {
		case "message":
			userPrompt = opt.String()
		case "model":
			modelOption = opt.String()
		}
	}

	// 2. Validate prompt (initial validation before calling service)
	if userPrompt == "" {
		c.logger.Warn("User prompt is empty")
		errMsg := "Your message prompt cannot be empty."
		resp := api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString(errMsg),
				Flags:   discord.EphemeralMessage,
			},
		}
		if err := s.RespondInteraction(e.ID, e.Token, resp); err != nil {
			c.logger.Error("Failed to send ephemeral error for empty prompt", zap.Error(err))
		}
		return errors.New("prompt is empty") // Return error to stop further processing
	}

	// 3. Validate model configuration (initial validation before calling service)
	if len(c.cfg.OpenAI.Models) == 0 {
		c.logger.Error("No OpenAI models configured")
		errMsg := "Error: No AI models are configured. Please contact an administrator."
		resp := api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString(errMsg),
				Flags:   discord.EphemeralMessage,
			},
		}
		if err := s.RespondInteraction(e.ID, e.Token, resp); err != nil {
			c.logger.Error("Failed to send ephemeral error for no models configured", zap.Error(err))
		}
		return errors.New("no openai models configured") // Return error to stop further processing
	}

	// 4. Delegate to the chat service
	// The service will handle the rest: creating thread, calling OpenAI, sending messages, caching.
	err := c.chatService.HandleChatInteraction(ctx, e, userPrompt, modelOption)
	if err != nil {
		// The service itself logs detailed errors.
		// The service also attempts to inform the user in the thread if possible.
		// If the error is something like "failed to send initial interaction response",
		// then an ephemeral message has already been attempted by the service.
		// For other errors (e.g., OpenAI call failed after thread creation), the service
		// tries to send a message to the thread.
		// We log a general error here.
		c.logger.Error("Chat service reported an error",
			zap.Error(err),
			zap.String("userPrompt", userPrompt),
			zap.String("modelOption", modelOption),
		)
		// Depending on the error type, we might want to send a generic ephemeral message here
		// if the service hasn't already. However, the service tries to handle this.
		// For now, just return the error. The bot's main interaction handler might log it.
		return fmt.Errorf("chat interaction failed: %w", err)
	}

	c.logger.Info("Chat command execution successfully delegated to chat service", zap.String("user", e.Member.User.Username))
	return nil
}
