package commands

import (
	"log"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
)

// Command defines the interface for a slash command.
type Command interface {
	Name() string
	Description() string
	Execute(s *session.Session, e *gateway.InteractionCreateEvent, i *discord.CommandInteraction) error
	CommandData() api.CreateCommandData
}

// commandRegistry holds all registered slash commands.
var commandRegistry = make(map[string]Command)

// RegisterCommand adds a command to the registry.
func RegisterCommand(cmd Command) {
	commandRegistry[cmd.Name()] = cmd
	log.Printf("Registered command: /%s", cmd.Name())
}

// GetCommand retrieves a command from the registry.
func GetCommand(name string) (Command, bool) {
	cmd, ok := commandRegistry[name]
	return cmd, ok
}

// CommandManager handles the registration of slash commands with Discord.
type CommandManager struct {
	session       *session.Session
	applicationID discord.AppID
}

// NewCommandManager creates a new CommandManager.
func NewCommandManager(s *session.Session, appID discord.Snowflake) *CommandManager {
	return &CommandManager{
		session:       s,
		applicationID: discord.AppID(appID),
	}
}

// RegisterCommands registers all commands in the commandRegistry with Discord.
func (cm *CommandManager) RegisterCommands() {
	for _, cmd := range commandRegistry {
		_, err := cm.session.CreateCommand(cm.applicationID, cmd.CommandData())
		if err != nil {
			log.Printf("Failed to register command /%s: %v", cmd.Name(), err)
		} else {
			log.Printf("Successfully registered command /%s with Discord", cmd.Name())
		}
	}
}
