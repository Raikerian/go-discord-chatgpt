package bot

import (
	"context" // Added context

	"github.com/Raikerian/go-discord-chatgpt/internal/commands"
	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"go.uber.org/zap" // Added zap
)

// Updated handleInteraction to accept context and logger
func handleInteraction(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, logger *zap.Logger) {
	// Check if it\'s a slash command
	switch data := e.Data.(type) {
	case *discord.CommandInteraction:
		logger.Info("Received slash command", zap.String("commandName", data.Name), zap.String("user", e.Member.User.Username)) // Replaced log

		// Get the command handler
		// Assuming GetCommand is updated or CommandManager is used here if it now holds commands
		cmd, ok := commands.GetCommand(data.Name) // This might need to change if commands are now managed via CommandManager instance
		if !ok {
			logger.Warn("Unknown command", zap.String("commandName", data.Name)) // Replaced log
			// Optionally send a response back to the user indicating the command is not found
			err := s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
				Type: api.MessageInteractionWithSource,
				Data: &api.InteractionResponseData{
					Content: option.NewNullableString("Command not found."),
				},
			})
			if err != nil {
				logger.Error("Failed to respond to interaction for unknown command", zap.Error(err)) // Replaced log
			}
			return
		}

		// Execute the command
		// Pass context and logger to the command execution if the interface supports it.
		// For now, assuming Execute does not take context/logger, but ideally it would.
		// If CommandExecute needs logger, it should be part of the Command interface and struct.
		err := cmd.Execute(s, e, data) // This would ideally be cmd.Execute(ctx, s, e, data, logger)
		if err != nil {
			logger.Error("Error executing command", zap.String("commandName", data.Name), zap.Error(err)) // Replaced log
			// Optionally send an error response back to the user
			errResp := s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
				Type: api.MessageInteractionWithSource,
				Data: &api.InteractionResponseData{
					Content: option.NewNullableString("An error occurred while executing the command."),
				},
			})
			if errResp != nil {
				logger.Error("Failed to send error response for command execution", zap.Error(errResp)) // Replaced log
			}
		} else {
			logger.Info("Command executed successfully", zap.String("commandName", data.Name))
		}

	default:
		logger.Debug("Received unhandled interaction type", zap.Any("type", e.Data)) // Replaced log
	}
}
