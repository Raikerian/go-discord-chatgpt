package commands

import (
	"context"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
)

// AppVersion is the version of the application, should be set during build time.
var AppVersion = "dev"

// VersionCommand is a command that responds with the application version.
type VersionCommand struct{}

// NewVersionCommand creates a new VersionCommand instance.
// This constructor will be used by Fx.
func NewVersionCommand() Command {
	return &VersionCommand{}
}

// Name returns the name of the command.
func (c *VersionCommand) Name() string {
	return "version"
}

// Description returns the description of the command.
func (c *VersionCommand) Description() string {
	return "Displays the current version of the bot."
}

// Options returns the command options.
func (c *VersionCommand) Options() []discord.CommandOption {
	return nil // No options for this command
}

// Execute runs the command.
func (c *VersionCommand) Execute(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error {
	return s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString("Version: " + AppVersion),
		},
	})
}
