package commands

import (
	"context"

	"github.com/diamondburned/arikawa/v3/api"     // For InteractionResponse, InteractionResponseData
	"github.com/diamondburned/arikawa/v3/discord" // For CommandInteraction, CommandOption
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
)

// PingCommand is a simple command that responds with "Pong!".
type PingCommand struct{}

// NewPingCommand creates a new PingCommand instance.
// This constructor will be used by Fx.
func NewPingCommand() Command {
	return &PingCommand{}
}

// Name returns the name of the command.
func (c *PingCommand) Name() string {
	return "ping"
}

// Description returns the description of the command.
func (c *PingCommand) Description() string {
	return "Responds with Pong!"
}

// Options returns the command options.
func (c *PingCommand) Options() []discord.CommandOption {
	return nil // No options for this command
}

// Execute runs the command.
func (c *PingCommand) Execute(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error {
	err := s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString("Pong!"),
		},
	})
	if err != nil {
		return err
	}
	return nil
}
