package commands

import (
	"github.com/diamondburned/arikawa/v3/api"     // For CreateCommandData, CommandOption, and API functions
	"github.com/diamondburned/arikawa/v3/discord" // For AppID, Interaction, CommandOption, GuildID
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"go.uber.org/zap"
)

// Command defines the interface for slash commands.
type Command interface {
	Name() string
	Description() string
	Options() []discord.CommandOption // Changed to discord.CommandOption for defining command parameters
	Execute(s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error
}

var registeredCommands = make(map[string]Command)

// RegisterCommand registers a new slash command.
func RegisterCommand(cmd Command) {
	registeredCommands[cmd.Name()] = cmd
}

// GetCommand retrieves a registered command by its name.
func GetCommand(name string) (Command, bool) {
	cmd, ok := registeredCommands[name]
	return cmd, ok
}

// CommandManager handles the registration of slash commands with Discord.
type CommandManager struct {
	session       *session.Session
	applicationID discord.AppID
	logger        *zap.Logger
}

// NewCommandManager creates a new CommandManager.
func NewCommandManager(s *session.Session, appID discord.AppID, logger *zap.Logger) *CommandManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger.Info("Creating new CommandManager")
	return &CommandManager{
		session:       s,
		applicationID: appID,
		logger:        logger,
	}
}

// RegisterCommands registers all loaded commands with Discord for the specified guilds.
func (cm *CommandManager) RegisterCommands(guildIDs []discord.GuildID) {
	cm.logger.Info("Registering slash commands with Discord for specified guilds...")
	var cmds []api.CreateCommandData
	for _, cmd := range registeredCommands {
		cmds = append(cmds, api.CreateCommandData{
			Name:        cmd.Name(),
			Description: cmd.Description(),
			Options:     cmd.Options(),
		})
		cm.logger.Debug("Preparing to register command", zap.String("commandName", cmd.Name()))
	}

	if len(cmds) == 0 {
		cm.logger.Info("No commands to register.")
		return
	}

	for _, guildID := range guildIDs {
		// Corrected to call BulkOverwriteGuildCommands as a method of cm.session
		registered, err := cm.session.BulkOverwriteGuildCommands(cm.applicationID, guildID, cmds)
		if err != nil {
			cm.logger.Error("Failed to bulk overwrite commands for guild",
				zap.Error(err),
				zap.Stringer("applicationID", cm.applicationID),
				zap.Stringer("guildID", guildID),
			)
			continue // Continue to the next guild
		}
		cm.logger.Info("Successfully registered slash commands for guild",
			zap.Int("count", len(registered)),
			zap.Stringer("applicationID", cm.applicationID),
			zap.Stringer("guildID", guildID),
		)
	}
}

// UnregisterAllCommands unregisters all commands for the specified guilds.
func (cm *CommandManager) UnregisterAllCommands(guildIDs []discord.GuildID) {
	cm.logger.Info("Unregistering all slash commands for specified guilds...", zap.Stringer("applicationID", cm.applicationID))

	for _, guildID := range guildIDs {
		// Corrected to call BulkOverwriteGuildCommands as a method of cm.session
		_, err := cm.session.BulkOverwriteGuildCommands(cm.applicationID, guildID, []api.CreateCommandData{})
		if err != nil {
			cm.logger.Error("Failed to unregister commands for guild",
				zap.Error(err),
				zap.Stringer("applicationID", cm.applicationID),
				zap.Stringer("guildID", guildID),
			)
			continue // Continue to the next guild
		}
		cm.logger.Info("Successfully requested to unregister all slash commands for guild",
			zap.Stringer("applicationID", cm.applicationID),
			zap.Stringer("guildID", guildID),
		)
	}
}
