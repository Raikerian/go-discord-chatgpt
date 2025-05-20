package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/Raikerian/go-discord-chatgpt/internal/gpt"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"

	"github.com/sashabaranov/go-openai"

	"go.uber.org/zap"
)

const (
	// gptDiscordTypingIndicatorCooldownSeconds is the cooldown for sending typing indicators.
	gptDiscordTypingIndicatorCooldownSeconds = 10
)

// Service handles the core logic for chat interactions.
// It uses OpenAI and a message cache.
type Service struct {
	logger        *zap.Logger
	cfg           *config.Config
	openaiClient  *openai.Client
	messagesCache *gpt.MessagesCache
}

// NewService creates a new chat Service.
func NewService(logger *zap.Logger, cfg *config.Config, openaiClient *openai.Client, messagesCache *gpt.MessagesCache) *Service {
	return &Service{
		logger:        logger.Named("chat_service"),
		cfg:           cfg,
		openaiClient:  openaiClient,
		messagesCache: messagesCache,
	}
}

// HandleChatInteraction processes a chat interaction, creates a thread, interacts with OpenAI, and responds.
func (s *Service) HandleChatInteraction(ctx context.Context, ses *session.Session, e *gateway.InteractionCreateEvent, userPrompt, modelOption string) error {
	s.logger.Info("Chat interaction processing started",
		zap.String("user", e.Member.User.Username),
		zap.String("userID", e.Member.User.ID.String()),
		zap.String("userPrompt", userPrompt),
		zap.String("modelOption", modelOption),
	)

	if userPrompt == "" {
		s.logger.Warn("User prompt is empty in service layer")
		return errors.New("prompt is empty") // Error already handled by caller
	}

	modelToUse, err := s.determineModel(modelOption)
	if err != nil {
		s.logger.Error("Failed to determine model", zap.Error(err))
		// Error already handled by caller
		return err
	}
	s.logger.Info("Determined model for chat", zap.String("modelToUse", modelToUse))

	summaryMessage := fmt.Sprintf(
		"Starting new chat session with %s!\n**User:** %s\n**Prompt:** %s\n**Model:** %s\n\nFuture messages in this thread will continue the conversation.",
		e.Member.User.Username,
		e.Member.User.Mention(),
		userPrompt,
		modelToUse,
	)

	originalMessage, err := s.sendInitialInteractionResponse(ses, e, summaryMessage)
	if err != nil {
		return err // Error already logged and handled in the helper
	}
	s.logger.Info("Initial interaction response sent and fetched",
		zap.String("messageID", originalMessage.ID.String()),
		zap.String("channelID", originalMessage.ChannelID.String()),
	)

	threadName := MakeThreadName(e.Member.User.Username, userPrompt, 100)
	newThread, err := s.createThread(ses, e, originalMessage, threadName, summaryMessage)
	if err != nil {
		return err // Error already logged and handled in the helper
	}
	s.logger.Info("Thread created successfully",
		zap.String("threadID", newThread.ID.String()),
		zap.String("threadName", newThread.Name),
	)

	stopTypingIndicator := s.manageTypingIndicator(ses, newThread.ID)
	defer func() { stopTypingIndicator <- true }()

	aiMessageContent, err := s.getOpenAIResponse(ctx, userPrompt, modelToUse, newThread.ID)
	if err != nil {
		// Error logged in helper, send error message to thread
		errMsgToThread := "Sorry, I encountered an error trying to reach the AI. Please try again later."
		if _, sendErr := ses.SendMessageComplex(newThread.ID, api.SendMessageData{Content: errMsgToThread}); sendErr != nil {
			s.logger.Error("Failed to send error message to thread after OpenAI failure", zap.Error(sendErr), zap.String("threadID", newThread.ID.String()))
		}
		return err
	}

	// Attempt to post AI response to the thread.
	if err := SendLongMessage(ses, newThread.ID, aiMessageContent); err != nil {
		s.logger.Error("Failed to send AI response to thread", zap.Error(err), zap.String("threadID", newThread.ID.String()))
		// Note: This error means the AI's message didn't make it to the thread,
		// even if the thread itself was successfully created. We don't return this error
		// to the main interaction handler as the thread itself is usable.
	}

	s.storeMessagesInCache(newThread.ID.String(), userPrompt, aiMessageContent, modelToUse)

	s.logger.Info("Chat interaction processing completed successfully", zap.String("threadID", newThread.ID.String()))
	return nil
}

// determineModel validates model configuration and selects the model to use.
func (s *Service) determineModel(modelOption string) (string, error) {
	if len(s.cfg.OpenAI.Models) == 0 {
		return "", errors.New("no OpenAI models configured")
	}

	if modelOption != "" {
		for _, configuredModel := range s.cfg.OpenAI.Models {
			if modelOption == configuredModel {
				return modelOption, nil
			}
		}
		s.logger.Warn("User specified an invalid model, defaulting.",
			zap.String("specifiedModel", modelOption),
			zap.Strings("availableModels", s.cfg.OpenAI.Models),
		)
	}
	return s.cfg.OpenAI.Models[0], nil // Default to the first configured model
}

// sendInitialInteractionResponse sends the first response to the interaction.
func (s *Service) sendInitialInteractionResponse(ses *session.Session, e *gateway.InteractionCreateEvent, summaryMessage string) (*discord.Message, error) {
	initialResponseData := api.InteractionResponseData{
		Content: option.NewNullableString(summaryMessage),
	}
	initialResponse := api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &initialResponseData,
	}

	if err := ses.RespondInteraction(e.ID, e.Token, initialResponse); err != nil {
		s.logger.Error("Failed to send initial interaction response", zap.Error(err))
		errMsg := "Sorry, I couldn't start the chat. Please try again."
		_, followUpErr := ses.FollowUpInteraction(e.AppID, e.Token, api.InteractionResponseData{
			Content: option.NewNullableString(errMsg),
			Flags:   discord.EphemeralMessage,
		})
		if followUpErr != nil {
			s.logger.Error("Failed to send error follow-up for initial response failure", zap.Error(followUpErr))
		}
		return nil, fmt.Errorf("failed to send initial interaction response: %w", err)
	}

	originalMessage, err := ses.InteractionResponse(e.AppID, e.Token)
	if err != nil {
		s.logger.Error("Failed to get the initial interaction response message", zap.Error(err))
		return nil, fmt.Errorf("failed to get interaction response message: %w", err)
	}
	return originalMessage, nil
}

// createThread creates a new thread from the original interaction response.
func (s *Service) createThread(ses *session.Session, e *gateway.InteractionCreateEvent, originalMessage *discord.Message, threadName, summaryMessage string) (*discord.Channel, error) {
	threadCreateAPIData := api.StartThreadData{
		Name:                threadName,
		AutoArchiveDuration: discord.ArchiveDuration(60), // TODO: Make configurable?
	}

	s.logger.Info("Attempting to create thread from message",
		zap.String("threadName", threadCreateAPIData.Name),
		zap.String("messageID", originalMessage.ID.String()),
		zap.String("channelID", originalMessage.ChannelID.String()),
	)

	newThread, err := ses.StartThreadWithMessage(originalMessage.ChannelID, originalMessage.ID, threadCreateAPIData)
	if err != nil {
		s.logger.Error("Failed to create thread from message", zap.Error(err))
		errMsgContent := fmt.Sprintf("%s\n\n**(Sorry, I couldn't create a discussion thread for this chat. Please try again or contact an administrator if the issue persists.)**", summaryMessage)
		_, editErr := ses.EditInteractionResponse(e.AppID, e.Token, api.EditInteractionResponseData{
			Content: option.NewNullableString(errMsgContent),
		})
		if editErr != nil {
			s.logger.Error("Failed to edit interaction response to indicate thread creation failure", zap.Error(editErr))
		}
		return nil, fmt.Errorf("failed to create thread from message: %w", err)
	}
	return newThread, nil
}

// manageTypingIndicator sends typing indicators periodically and returns a channel to stop it.
func (s *Service) manageTypingIndicator(ses *session.Session, threadID discord.ChannelID) chan<- bool {
	sendTyping := func() {
		if err := ses.Typing(threadID); err != nil {
			s.logger.Warn("Failed to send typing indicator", zap.Error(err), zap.String("threadID", threadID.String()))
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
	return stopTypingIndicator
}

// getOpenAIResponse sends the prompt to OpenAI and returns the AI's message content.
func (s *Service) getOpenAIResponse(ctx context.Context, userPrompt, modelToUse string, threadID discord.ChannelID) (string, error) {
	s.logger.Info("Sending prompt to OpenAI",
		zap.String("userPrompt", userPrompt),
		zap.String("modelToUse", modelToUse),
		zap.String("threadID", threadID.String()),
	)

	aiRequest := openai.ChatCompletionRequest{
		Model: modelToUse,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
	}

	aiResponse, err := s.openaiClient.CreateChatCompletion(ctx, aiRequest)
	if err != nil {
		s.logger.Error("Failed to get response from OpenAI", zap.Error(err), zap.String("threadID", threadID.String()))
		return "", fmt.Errorf("failed to create chat completion: %w", err)
	}

	if len(aiResponse.Choices) == 0 || aiResponse.Choices[0].Message.Content == "" {
		s.logger.Warn("OpenAI returned an empty response", zap.Any("aiResponse", aiResponse), zap.String("threadID", threadID.String()))
		return "", errors.New("OpenAI returned empty response")
	}

	aiMessageContent := aiResponse.Choices[0].Message.Content
	s.logger.Info("Received response from OpenAI",
		zap.Int("promptTokens", aiResponse.Usage.PromptTokens),
		zap.Int("completionTokens", aiResponse.Usage.CompletionTokens),
		zap.Int("totalTokens", aiResponse.Usage.TotalTokens),
		zap.String("threadID", threadID.String()),
	)
	return aiMessageContent, nil
}

// storeMessagesInCache stores the user prompt and AI response in the message cache.
func (s *Service) storeMessagesInCache(threadIDStr string, userPrompt, aiMessageContent, modelUsed string) {
	if s.messagesCache != nil {
		history := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
			{Role: openai.ChatMessageRoleAssistant, Content: aiMessageContent},
		}
		cacheData := &gpt.MessagesCacheData{
			Messages: history,
			Model:    modelUsed,
		}
		s.messagesCache.Add(threadIDStr, cacheData)
		s.logger.Debug("Stored initial messages in cache", zap.String("threadID", threadIDStr))
	}
}
