package commands

import (
	"log"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
)

// PingCommand is a simple ping command.
type PingCommand struct{}

// Name returns the command's name.
func (c *PingCommand) Name() string {
	return "ping"
}

// Description returns the command's description.
func (c *PingCommand) Description() string {
	return "Responds with Pong!"
}

// CommandData builds the command data for Discord API.
func (c *PingCommand) CommandData() api.CreateCommandData {
	return api.CreateCommandData{
		Name:        c.Name(),
		Description: c.Description(),
	}
}

// Execute runs the command.
func (c *PingCommand) Execute(s *session.Session, e *gateway.InteractionCreateEvent, i *discord.CommandInteraction) error {
	log.Printf("Executing ping command for interaction ID: %s", e.ID)
	return s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString("Pong!"),
		},
	})
}

// init registers the command when the package is initialized.
func init() {
	RegisterCommand(&PingCommand{})
}
