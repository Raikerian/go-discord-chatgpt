package bot

import (
	"context"

	"github.com/Raikerian/go-discord-chatgpt/internal/commands"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"

	"go.uber.org/zap"
)

func handleInteraction(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, logger *zap.Logger, cmdManager *commands.CommandManager) {
	// Check if it's a slash command
	switch data := e.Data.(type) {
	case *discord.CommandInteraction:
		logger.Info("Received slash command", zap.String("commandName", data.Name), zap.String("user", e.Member.User.Username))

		// Get the command handler
		cmd, ok := cmdManager.GetCommand(data.Name)
		if !ok {
			logger.Warn("Unknown command", zap.String("commandName", data.Name))
			// Optionally send a response back to the user indicating the command is not found
			err := s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
				Type: api.MessageInteractionWithSource,
				Data: &api.InteractionResponseData{
					Content: option.NewNullableString("Command not found."),
				},
			})
			if err != nil {
				logger.Error("Failed to respond to interaction for unknown command", zap.Error(err))
			}
			return
		}

		// Execute the command
		err := cmd.Execute(ctx, s, e, data)
		if err != nil {
			logger.Error("Error executing command", zap.String("commandName", data.Name), zap.Error(err))
			// Optionally send an error response back to the user
			errResp := s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
				Type: api.MessageInteractionWithSource,
				Data: &api.InteractionResponseData{
					Content: option.NewNullableString("An error occurred while executing the command."),
				},
			})
			if errResp != nil {
				logger.Error("Failed to send error response for command execution", zap.Error(errResp))
			}
		} else {
			logger.Info("Command executed successfully", zap.String("commandName", data.Name))
		}

	default:
		logger.Debug("Received unhandled interaction type", zap.Any("type", e.Data))
	}
}
