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

// handleMessageCreate handles incoming messages, specifically for thread interactions.
// It is called by the event handler in the Bot struct.
func (b *Bot) handleMessageCreate(ctx context.Context, s *session.Session, e *gateway.MessageCreateEvent) {
	// Ignore bot's own messages
	selfUser, err := s.Me()
	if err != nil {
		b.Logger.Error("Failed to get self user information", zap.Error(err))

		return
	}
	if e.Author.ID == selfUser.ID {
		return
	}

	// Check if the message is in a thread by fetching channel info
	ch, err := s.Channel(e.ChannelID) // Use the session from the event handler context
	if err != nil {
		b.Logger.Warn("Failed to fetch channel info for MessageCreateEvent", zap.Error(err), zap.String("channelID", e.ChannelID.String()))

		return
	}

	// Use discord.GuildAnnouncementThread as discord.GuildNewsThread is deprecated.
	isThread := ch.Type == discord.GuildPublicThread || ch.Type == discord.GuildPrivateThread || ch.Type == discord.GuildAnnouncementThread
	if !isThread {
		b.Logger.Debug("Message is not in a thread, ignoring", zap.String("messageID", e.ID.String()))

		return
	}

	b.Logger.Info("Received message in a thread",
		zap.String("threadID", e.ChannelID.String()),
		zap.String("authorID", e.Author.ID.String()),
		zap.String("content", e.Content),
	)

	// Delegate to chat.Service
	if b.ChatService == nil {
		b.Logger.Error("Chat service is not initialized in Bot, cannot handle thread message")

		return
	}

	if err := b.ChatService.HandleThreadMessage(ctx, e); err != nil {
		b.Logger.Error("Error handling thread message via chat service",
			zap.Error(err),
			zap.String("threadID", e.ChannelID.String()),
		)
	}
}

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
