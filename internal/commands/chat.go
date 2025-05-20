package commands

import (
	"context"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"go.uber.org/zap"
)

// ChatCommand is a boilerplate for the /chat command.
type ChatCommand struct {
	logger *zap.Logger
}

// NewChatCommand creates a new ChatCommand.
func NewChatCommand(logger *zap.Logger) Command {
	return &ChatCommand{
		logger: logger.Named("chat_command"),
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

// Options returns the options for the command.
func (c *ChatCommand) Options() []discord.CommandOption {
	return []discord.CommandOption{
		&discord.StringOption{
			OptionName:  "message",
			Description: "Your message to ChatGPT",
			Required:    true,
		},
	}
}

// Execute executes the command.
func (c *ChatCommand) Execute(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error {
	c.logger.Info("Chat command executed", zap.String("user", e.Member.User.Username))

	// For now, do nothing and send a placeholder response.
	// In the future, this will interact with the GPT service.
	return s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString("Chat command received! (Not implemented yet)"),
		},
	})
}
