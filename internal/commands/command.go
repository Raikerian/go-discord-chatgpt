package commands

import (
	"context"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
)

// Command defines the interface for slash commands.
type Command interface {
	Name() string
	Description() string
	Options() []discord.CommandOption
	Execute(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error
}
