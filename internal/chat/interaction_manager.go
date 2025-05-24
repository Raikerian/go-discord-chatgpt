package chat

import (
	"fmt"
	"time"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"go.uber.org/zap"
)

const (
	// gptDiscordTypingIndicatorCooldownSeconds is the cooldown for sending typing indicators.
	gptDiscordTypingIndicatorCooldownSeconds = 10
)

// DiscordInteractionManager handles direct interactions with the Discord API related to chat flow.
type DiscordInteractionManager interface {
	// SendInitialResponse sends the first (source) response to an interaction.
	SendInitialResponse(ses *session.Session, eventID discord.InteractionID, eventToken string, appID discord.AppID, summaryMessage string) (*discord.Message, error)
	// CreateThreadForInteraction creates a new public thread from an original interaction response message.
	CreateThreadForInteraction(ses *session.Session, originalMessage *discord.Message, appID discord.AppID, eventToken string, threadName, originalSummaryMessageForFallback string) (*discord.Channel, error)
	// StartTypingIndicator sends typing indicators periodically until the returned stop function is called.
	StartTypingIndicator(ses *session.Session, channelID discord.ChannelID) (stopFunc func())
	// SendMessage sends a message to a channel, handling long messages by splitting them.
	SendMessage(ses *session.Session, channelID discord.ChannelID, content string) error
}

// NewDiscordInteractionManager creates a new instance of DiscordInteractionManager.
func NewDiscordInteractionManager(logger *zap.Logger) DiscordInteractionManager {
	return &discordInteractionManagerImpl{
		logger: logger.Named("discord_interaction_manager"),
	}
}

type discordInteractionManagerImpl struct {
	logger *zap.Logger
}

// SendInitialResponse sends the first response to the interaction.
func (dim *discordInteractionManagerImpl) SendInitialResponse(ses *session.Session, eventID discord.InteractionID, eventToken string, appID discord.AppID, summaryMessage string) (*discord.Message, error) {
	initialResponseData := api.InteractionResponseData{
		Content: option.NewNullableString(summaryMessage),
	}
	initialResponse := api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &initialResponseData,
	}

	if err := ses.RespondInteraction(eventID, eventToken, initialResponse); err != nil {
		dim.logger.Error("Failed to send initial interaction response", zap.Error(err))
		errMsg := "Sorry, I couldn't start the chat. Please try again."
		_, followUpErr := ses.FollowUpInteraction(appID, eventToken, api.InteractionResponseData{
			Content: option.NewNullableString(errMsg),
			Flags:   discord.EphemeralMessage,
		})
		if followUpErr != nil {
			dim.logger.Error("Failed to send error follow-up for initial response failure", zap.Error(followUpErr))
		}

		return nil, fmt.Errorf("failed to send initial interaction response: %w", err)
	}

	originalMessage, err := ses.InteractionResponse(appID, eventToken)
	if err != nil {
		dim.logger.Error("Failed to get the initial interaction response message", zap.Error(err))

		return nil, fmt.Errorf("failed to get interaction response message: %w", err)
	}

	return originalMessage, nil
}

// CreateThreadForInteraction creates a new thread from the original interaction response.
func (dim *discordInteractionManagerImpl) CreateThreadForInteraction(ses *session.Session, originalMessage *discord.Message, appID discord.AppID, eventToken string, threadName, originalSummaryMessageForFallback string) (*discord.Channel, error) {
	threadCreateAPIData := api.StartThreadData{
		Name:                threadName,
		AutoArchiveDuration: discord.ArchiveDuration(60), // TODO: Make configurable?
	}

	dim.logger.Info("Attempting to create thread from message",
		zap.String("threadName", threadCreateAPIData.Name),
		zap.String("messageID", originalMessage.ID.String()),
		zap.String("channelID", originalMessage.ChannelID.String()),
	)

	newThread, err := ses.StartThreadWithMessage(originalMessage.ChannelID, originalMessage.ID, threadCreateAPIData)
	if err != nil {
		dim.logger.Error("Failed to create thread from message", zap.Error(err))
		errMsgContent := originalSummaryMessageForFallback + "\n\n**(Sorry, I couldn't create a discussion thread for this chat. Please try again or contact an administrator if the issue persists.)**"
		_, editErr := ses.EditInteractionResponse(appID, eventToken, api.EditInteractionResponseData{
			Content: option.NewNullableString(errMsgContent),
		})
		if editErr != nil {
			dim.logger.Error("Failed to edit interaction response to indicate thread creation failure", zap.Error(editErr))
		}

		return nil, fmt.Errorf("failed to create thread from message: %w", err)
	}

	return newThread, nil
}

// StartTypingIndicator sends typing indicators periodically and returns a stop function.
func (dim *discordInteractionManagerImpl) StartTypingIndicator(ses *session.Session, channelID discord.ChannelID) (stopFunc func()) {
	sendTyping := func() {
		if err := ses.Typing(channelID); err != nil {
			dim.logger.Warn("Failed to send typing indicator", zap.Error(err), zap.String("channelID", channelID.String()))
		}
	}

	sendTyping() // Indicate bot is thinking immediately

	typingTicker := time.NewTicker(gptDiscordTypingIndicatorCooldownSeconds * time.Second)
	stopTypingIndicator := make(chan bool, 1)

	go func() {
		defer typingTicker.Stop()
		for {
			select {
			case <-typingTicker.C:
				sendTyping()
			case <-stopTypingIndicator:
				return
			}
		}
	}()

	return func() {
		stopTypingIndicator <- true
	}
}

// SendMessage sends a message to a channel, handling long messages by splitting them.
func (dim *discordInteractionManagerImpl) SendMessage(ses *session.Session, channelID discord.ChannelID, content string) error {
	return SendLongMessage(ses, channelID, content)
}
