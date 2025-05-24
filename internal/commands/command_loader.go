package commands

import (
	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/session"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

// CommandManager handles the registration of slash commands with Discord.
type CommandManager struct {
	session       *session.Session
	applicationID discord.AppID
	logger        *zap.Logger
	commandMap    map[string]Command // Internal map to store commands
}

// CommandManagerParams holds the dependencies for NewCommandManager.
type CommandManagerParams struct {
	fx.In
	Session       *session.Session
	ApplicationID discord.AppID
	Logger        *zap.Logger
	Commands      []Command `group:"commands"` // Injected by Fx
}

// NewCommandManager creates a new CommandManager.
func NewCommandManager(params CommandManagerParams) *CommandManager {
	if params.Logger == nil {
		params.Logger = zap.NewNop()
	}
	params.Logger.Info("Creating new CommandManager with commands from Fx group")

	cm := &CommandManager{
		session:       params.Session,
		applicationID: params.ApplicationID,
		logger:        params.Logger,
		commandMap:    make(map[string]Command),
	}

	for _, cmd := range params.Commands {
		if cmd == nil {
			params.Logger.Warn("Received a nil command from Fx group")

			continue
		}
		if _, exists := cm.commandMap[cmd.Name()]; exists {
			params.Logger.Warn("Duplicate command name provided by Fx", zap.String("commandName", cmd.Name()))

			continue
		}
		cm.commandMap[cmd.Name()] = cmd
		params.Logger.Debug("Loaded command via Fx", zap.String("commandName", cmd.Name()))
	}
	params.Logger.Info("CommandManager created", zap.Int("numberOfCommandsLoaded", len(cm.commandMap)))

	return cm
}

// GetCommand retrieves a registered command by its name.
func (cm *CommandManager) GetCommand(name string) (Command, bool) {
	cmd, ok := cm.commandMap[name]
	if !ok {
		cm.logger.Warn("Attempted to get unknown command", zap.String("commandName", name))
	}

	return cmd, ok
}

// RegisterCommands registers all loaded commands with Discord for the specified guilds.
func (cm *CommandManager) RegisterCommands(guildIDs []discord.GuildID) {
	cm.logger.Info("Registering slash commands with Discord for specified guilds...", zap.Int("commandCount", len(cm.commandMap)))
	cmdsToRegister := make([]api.CreateCommandData, 0, len(cm.commandMap))
	for _, cmd := range cm.commandMap {
		cmdsToRegister = append(cmdsToRegister, api.CreateCommandData{
			Name:        cmd.Name(),
			Description: cmd.Description(),
			Options:     cmd.Options(),
		})
		cm.logger.Debug("Preparing to register command", zap.String("commandName", cmd.Name()))
	}

	if len(cmdsToRegister) == 0 {
		cm.logger.Info("No commands to register.")

		return
	}

	// Global commands registration (if no guildIDs are specified)
	if len(guildIDs) == 0 {
		cm.logger.Info("No specific guild IDs provided, attempting to register commands globally.")
		registered, err := cm.session.BulkOverwriteCommands(cm.applicationID, cmdsToRegister)
		if err != nil {
			cm.logger.Error("Failed to bulk overwrite global commands",
				zap.Error(err),
				zap.Stringer("applicationID", cm.applicationID),
			)
		} else {
			cm.logger.Info("Successfully registered global slash commands",
				zap.Int("count", len(registered)),
				zap.Stringer("applicationID", cm.applicationID),
			)
		}

		return // Exit after attempting global registration
	}

	// Guild-specific commands registration
	for _, guildID := range guildIDs {
		registered, err := cm.session.BulkOverwriteGuildCommands(cm.applicationID, guildID, cmdsToRegister)
		if err != nil {
			cm.logger.Error("Failed to bulk overwrite commands for guild",
				zap.Error(err),
				zap.Stringer("applicationID", cm.applicationID),
				zap.Stringer("guildID", guildID),
			)

			continue
		}
		cm.logger.Info("Successfully registered slash commands for guild",
			zap.Int("count", len(registered)),
			zap.Stringer("applicationID", cm.applicationID),
			zap.Stringer("guildID", guildID),
		)
	}
}

// UnregisterAllCommands unregisters all commands for the specified guilds or globally.
func (cm *CommandManager) UnregisterAllCommands(guildIDs []discord.GuildID) {
	cm.logger.Info("Unregistering all slash commands...", zap.Stringer("applicationID", cm.applicationID))

	// Global commands unregistration
	if len(guildIDs) == 0 {
		cm.logger.Info("No specific guild IDs provided, attempting to unregister all global commands.")
		// Pass an empty slice to unregister all commands
		_, err := cm.session.BulkOverwriteCommands(cm.applicationID, []api.CreateCommandData{})
		if err != nil {
			cm.logger.Error("Failed to unregister global commands",
				zap.Error(err),
				zap.Stringer("applicationID", cm.applicationID),
			)
		} else {
			cm.logger.Info("Successfully requested to unregister all global slash commands.")
		}

		return // Exit after attempting global unregistration
	}

	// Guild-specific commands unregistration
	for _, guildID := range guildIDs {
		_, err := cm.session.BulkOverwriteGuildCommands(cm.applicationID, guildID, []api.CreateCommandData{})
		if err != nil {
			cm.logger.Error("Failed to unregister commands for guild",
				zap.Error(err),
				zap.Stringer("applicationID", cm.applicationID),
				zap.Stringer("guildID", guildID),
			)

			continue
		}
		cm.logger.Info("Successfully requested to unregister all slash commands for guild",
			zap.Stringer("applicationID", cm.applicationID),
			zap.Stringer("guildID", guildID),
		)
	}
}
