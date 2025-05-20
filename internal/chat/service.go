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

	// 1. Validate prompt (already done by caller, but good for service boundary)
	if userPrompt == "" {
		s.logger.Warn("User prompt is empty in service layer")
		// Error already handled by caller with ephemeral message
		return errors.New("prompt is empty")
	}

	// 2. Validate model configuration and determine model to use
	var modelToUse string
	if len(s.cfg.OpenAI.Models) == 0 {
		s.logger.Error("No OpenAI models configured")
		// Error already handled by caller with ephemeral message
		return errors.New("no OpenAI models configured")
	}

	if modelOption != "" {
		isValidModel := false
		for _, configuredModel := range s.cfg.OpenAI.Models {
			if modelOption == configuredModel {
				modelToUse = modelOption
				isValidModel = true
				break
			}
		}
		if !isValidModel {
			s.logger.Warn("User specified an invalid model, defaulting.",
				zap.String("specifiedModel", modelOption),
				zap.Strings("availableModels", s.cfg.OpenAI.Models),
			)
			modelToUse = s.cfg.OpenAI.Models[0] // Default to the first configured model
		}
	} else {
		modelToUse = s.cfg.OpenAI.Models[0] // Default to the first configured model
	}
	s.logger.Info("Determined model for chat", zap.String("modelToUse", modelToUse))

	// 3. Prepare thread name and initial message content
	threadName := MakeThreadName(e.Member.User.Username, userPrompt, 100)
	summaryMessage := fmt.Sprintf(
		"Starting new chat session with %s!\n**User:** %s\n**Prompt:** %s\n**Model:** %s\n\nFuture messages in this thread will continue the conversation.",
		e.Member.User.Username,
		e.Member.User.Mention(),
		userPrompt,
		modelToUse,
	)

	// 4. Send the initial interaction response (this will be the first message in the thread)
	initialResponseData := api.InteractionResponseData{
		Content: option.NewNullableString(summaryMessage),
	}
	initialResponse := api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &initialResponseData,
	}

	if err := ses.RespondInteraction(e.ID, e.Token, initialResponse); err != nil {
		s.logger.Error("Failed to send initial interaction response", zap.Error(err))
		// Attempt to send an ephemeral follow-up if the main response failed
		errMsg := "Sorry, I couldn't start the chat. Please try again."
		_, followUpErr := ses.FollowUpInteraction(e.AppID, e.Token, api.InteractionResponseData{
			Content: option.NewNullableString(errMsg),
			Flags:   discord.EphemeralMessage,
		})
		if followUpErr != nil {
			s.logger.Error("Failed to send error follow-up for initial response failure", zap.Error(followUpErr))
		}
		return fmt.Errorf("failed to send initial interaction response: %w", err)
	}

	// 5. Fetch the original interaction response message we just sent.
	originalMessage, err := ses.InteractionResponse(e.AppID, e.Token)
	if err != nil {
		s.logger.Error("Failed to get the initial interaction response message", zap.Error(err))
		return fmt.Errorf("failed to get interaction response message: %w", err)
	}
	s.logger.Info("Initial interaction response sent and fetched",
		zap.String("messageID", originalMessage.ID.String()),
		zap.String("channelID", originalMessage.ChannelID.String()),
	)

	// 6. Prepare thread creation data
	threadCreateAPIData := api.StartThreadData{
		Name:                threadName,
		AutoArchiveDuration: discord.ArchiveDuration(60),
	}

	s.logger.Info("Attempting to create thread from message",
		zap.String("threadName", threadCreateAPIData.Name),
		zap.String("messageID", originalMessage.ID.String()),
		zap.String("channelID", originalMessage.ChannelID.String()),
	)

	// 7. Start the thread from the message
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
		return fmt.Errorf("failed to create thread from message: %w", err)
	}

	s.logger.Info("Thread created successfully from message",
		zap.String("threadID", newThread.ID.String()),
		zap.String("threadName", newThread.Name),
		zap.String("parentMessageID", originalMessage.ID.String()),
	)

	// 8. Send initial prompt to OpenAI
	s.logger.Info("Sending initial prompt to OpenAI",
		zap.String("userPrompt", userPrompt),
		zap.String("modelToUse", modelToUse),
		zap.String("threadID", newThread.ID.String()),
	)

	// Helper function to send typing indicator
	sendTyping := func() {
		if err := ses.Typing(newThread.ID); err != nil {
			s.logger.Warn("Failed to send typing indicator", zap.Error(err), zap.String("threadID", newThread.ID.String()))
		}
	}

	// Indicate bot is thinking immediately
	sendTyping()

	// Start a ticker for subsequent typing indicators
	typingTicker := time.NewTicker(gptDiscordTypingIndicatorCooldownSeconds * time.Second)
	defer typingTicker.Stop()
	stopTypingIndicator := make(chan bool, 1) // Buffered channel to prevent goroutine leak

	go func() {
		for {
			select {
			case <-typingTicker.C:
				sendTyping()
			case <-stopTypingIndicator:
				return
			}
		}
	}()

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
		s.logger.Error("Failed to get response from OpenAI", zap.Error(err), zap.String("threadID", newThread.ID.String()))
		stopTypingIndicator <- true
		errMsgToThread := "Sorry, I encountered an error trying to reach the AI. Please try again later."
		if _, sendErr := ses.SendMessageComplex(newThread.ID, api.SendMessageData{Content: errMsgToThread}); sendErr != nil {
			s.logger.Error("Failed to send error message to thread after OpenAI failure", zap.Error(sendErr), zap.String("threadID", newThread.ID.String()))
		}
		return fmt.Errorf("failed to create chat completion: %w", err)
	}

	if len(aiResponse.Choices) == 0 || aiResponse.Choices[0].Message.Content == "" {
		s.logger.Warn("OpenAI returned an empty response", zap.Any("aiResponse", aiResponse), zap.String("threadID", newThread.ID.String()))
		stopTypingIndicator <- true
		noRespMsgToThread := "The AI didn't provide a response this time. You might want to try rephrasing your message."
		if _, sendErr := ses.SendMessageComplex(newThread.ID, api.SendMessageData{Content: noRespMsgToThread}); sendErr != nil {
			s.logger.Error("Failed to send empty AI response message to thread", zap.Error(sendErr), zap.String("threadID", newThread.ID.String()))
		}
		return errors.New("OpenAI returned empty response")
	}

	aiMessageContent := aiResponse.Choices[0].Message.Content
	s.logger.Info("Received response from OpenAI",
		zap.Int("promptTokens", aiResponse.Usage.PromptTokens),
		zap.Int("completionTokens", aiResponse.Usage.CompletionTokens),
		zap.Int("totalTokens", aiResponse.Usage.TotalTokens),
		zap.String("threadID", newThread.ID.String()),
	)

	stopTypingIndicator <- true

	// 9. Post AI's first answer to the thread, splitting if necessary
	if err := SendLongMessage(ses, newThread.ID, aiMessageContent); err != nil {
		s.logger.Error("Failed to send AI response to thread", zap.Error(err), zap.String("threadID", newThread.ID.String()))
		// The user has the thread, but the AI message failed to send. This is not ideal.
		// We won't return an error to the interaction itself as the thread is created.
		// However, it might be good to log this prominently or alert an admin.
	}

	// 10. Store the initial user prompt and AI response in the cache
	if s.messagesCache != nil {
		history := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
			{Role: openai.ChatMessageRoleAssistant, Content: aiMessageContent},
		}
		cacheData := &gpt.MessagesCacheData{
			Messages: history,
			Model:    modelToUse,
		}
		s.messagesCache.Add(newThread.ID.String(), cacheData)
		s.logger.Debug("Stored initial messages in cache", zap.String("threadID", newThread.ID.String()))
	}

	s.logger.Info("Chat interaction processing completed successfully", zap.String("threadID", newThread.ID.String()))
	return nil
}
