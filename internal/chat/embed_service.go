// Package chat provides modular chat service components for handling Discord chat interactions with AI.
package chat

import (
	"context"
	"fmt"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
)

const (
	// OpenAI icon URL from the repository.
	openAIIconURL = "https://raw.githubusercontent.com/Raikerian/go-discord-chatgpt/master/openai-black.png"
)

// MessageEmbedService defines the interface for managing Discord message embeds.
type MessageEmbedService interface {
	AddUsageFooter(ctx context.Context, message *discord.Message, usage openai.Usage, modelName string) error
}

// discordEmbedService implements the MessageEmbedService interface for Discord.
type discordEmbedService struct {
	session        *session.Session
	usageFormatter UsageFormatter
	logger         *zap.Logger
}

// NewDiscordEmbedService creates a new Discord embed service.
func NewDiscordEmbedService(ses *session.Session, usageFormatter UsageFormatter, logger *zap.Logger) MessageEmbedService {
	return &discordEmbedService{
		session:        ses,
		usageFormatter: usageFormatter,
		logger:         logger.Named("discord_embed_service"),
	}
}

// AddUsageFooter adds a usage footer embed to a Discord message.
func (s *discordEmbedService) AddUsageFooter(ctx context.Context, message *discord.Message, usage openai.Usage, modelName string) error {
	// Format usage information
	usageText, err := s.usageFormatter.FormatUsage(usage, modelName)
	if err != nil {
		s.logger.Warn("Failed to format usage information", zap.Error(err))
		// Fallback to basic token info without cost
		usageText = fmt.Sprintf("Completion: %d tokens, Total: %d tokens",
			usage.CompletionTokens, usage.TotalTokens)
	}

	// Create embed with footer
	embed := discord.Embed{
		Footer: &discord.EmbedFooter{
			Text: usageText,
			Icon: openAIIconURL,
		},
	}

	// Edit the message to add the embed
	editData := api.EditMessageData{
		Embeds: &[]discord.Embed{embed},
	}

	_, err = s.session.EditMessageComplex(message.ChannelID, message.ID, editData)
	if err != nil {
		s.logger.Warn("Failed to add usage footer to message",
			zap.Error(err),
			zap.String("messageID", message.ID.String()),
			zap.String("channelID", message.ChannelID.String()))

		return fmt.Errorf("failed to edit message with usage footer: %w", err)
	}

	s.logger.Debug("Successfully added usage footer to message",
		zap.String("messageID", message.ID.String()),
		zap.String("usageText", usageText))

	return nil
}
