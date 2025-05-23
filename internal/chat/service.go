package chat

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/sashabaranov/go-openai"

	"go.uber.org/zap"
)

// Service orchestrates chat interactions by coordinating various specialized services.
type Service struct {
	logger *zap.Logger
	cfg    *config.Config
	ses    *session.Session

	interactionManager DiscordInteractionManager
	aiProvider         AIProvider
	conversationStore  ConversationStore
	modelSelector      ModelSelector

	// ongoingRequests stores cancel functions for ongoing OpenAI requests per thread.
	// key: discord.ChannelID, value: context.CancelFunc
	ongoingRequests sync.Map
}

// NewService creates a new refactored chat Service.
func NewService(
	logger *zap.Logger,
	cfg *config.Config,
	ses *session.Session,
	interactionManager DiscordInteractionManager,
	aiProvider AIProvider,
	conversationStore ConversationStore,
	modelSelector ModelSelector,
) *Service {
	return &Service{
		logger:             logger.Named("chat_service_orchestrator"),
		cfg:                cfg,
		ses:                ses,
		interactionManager: interactionManager,
		aiProvider:         aiProvider,
		conversationStore:  conversationStore,
		modelSelector:      modelSelector,
	}
}

// HandleChatInteraction processes a new chat command.
func (s *Service) HandleChatInteraction(ctx context.Context, e *gateway.InteractionCreateEvent, userPrompt, modelOption string) error {
	s.logger.Info("Chat interaction processing started",
		zap.String("user", e.Member.User.Username),
		zap.String("userID", e.Member.User.ID.String()),
		zap.String("userPrompt", userPrompt),
		zap.String("modelOption", modelOption),
	)

	if userPrompt == "" {
		s.logger.Warn("User prompt is empty in service layer")
		return errors.New("prompt is empty")
	}

	modelToUse, err := s.modelSelector.SelectModel(modelOption)
	if err != nil {
		s.logger.Error("Failed to determine model", zap.Error(err))
		return err
	}
	s.logger.Info("Determined model for chat", zap.String("modelToUse", modelToUse))

	userDisplayName := GetUserDisplayName(e.Member.User)
	botDisplayName, err := s.getBotDisplayName()
	if err != nil {
		s.logger.Error("Failed to get bot display name", zap.Error(err))
		botDisplayName = defaultBotName
	}

	summaryMessage := fmt.Sprintf(
		"Starting new chat session with %s!\n**User:** %s\n**Prompt:** %s\n**Model:** %s\n\nFuture messages in this thread will continue the conversation.",
		e.Member.User.Username,
		e.Member.User.Mention(),
		userPrompt,
		modelToUse,
	)

	originalMessage, err := s.interactionManager.SendInitialResponse(s.ses, e.ID, e.Token, e.AppID, summaryMessage)
	if err != nil {
		return err
	}
	s.logger.Info("Initial interaction response sent and fetched",
		zap.String("messageID", originalMessage.ID.String()),
		zap.String("channelID", originalMessage.ChannelID.String()),
	)

	threadName := MakeThreadName(e.Member.User.Username, userPrompt, 100)
	newThread, err := s.interactionManager.CreateThreadForInteraction(s.ses, originalMessage, e.AppID, e.Token, threadName, summaryMessage)
	if err != nil {
		return err
	}
	s.logger.Info("Thread created successfully",
		zap.String("threadID", newThread.ID.String()),
		zap.String("threadName", newThread.Name),
	)

	stopTypingIndicator := s.interactionManager.StartTypingIndicator(s.ses, newThread.ID)
	defer stopTypingIndicator()

	// Prepare the OpenAI messages
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleUser,
			Content: userPrompt,
			Name:    SanitizeOpenAIName(userDisplayName),
		},
	}

	aiResponse, err := s.aiProvider.GetChatCompletion(ctx, modelToUse, messages)
	if err != nil {
		errMsgToThread := "Sorry, I encountered an error trying to reach the AI. Please try again later."
		if sendErr := s.interactionManager.SendMessage(s.ses, newThread.ID, errMsgToThread); sendErr != nil {
			s.logger.Error("Failed to send error message to thread after OpenAI failure", zap.Error(sendErr), zap.String("threadID", newThread.ID.String()))
		}
		return err
	}

	aiMessageContent := aiResponse.Choices[0].Message.Content

	if err := s.interactionManager.SendMessage(s.ses, newThread.ID, aiMessageContent); err != nil {
		s.logger.Error("Failed to send AI response to thread", zap.Error(err), zap.String("threadID", newThread.ID.String()))
	}

	s.conversationStore.StoreInitialConversation(newThread.ID.String(), userPrompt, aiMessageContent, modelToUse, userDisplayName, botDisplayName, SanitizeOpenAIName)

	s.logger.Info("Chat interaction processing completed successfully", zap.String("threadID", newThread.ID.String()))
	return nil
}

// HandleThreadMessage processes a follow-up message in an existing chat thread.
func (s *Service) HandleThreadMessage(ctx context.Context, evt *gateway.MessageCreateEvent) error {
	s.logger.Info("Handling thread message",
		zap.String("threadID", evt.ChannelID.String()),
		zap.String("authorID", evt.Author.ID.String()),
		zap.String("content", evt.Content),
	)
	threadIDStr := evt.ChannelID.String()

	// Negative Cache Check
	if s.conversationStore.IsInNegativeCache(threadIDStr) {
		s.logger.Debug("Thread is in negative cache, ignoring message", zap.String("threadID", threadIDStr))
		return nil
	}

	var modelToUse string
	cachedData, found := s.conversationStore.GetConversation(threadIDStr)

	if found {
		s.logger.Debug("Found conversation in primary cache", zap.String("threadID", threadIDStr))
		modelToUse = cachedData.Model
	} else {
		s.logger.Info("Conversation not in cache, attempting to reconstruct", zap.String("threadID", threadIDStr))
		selfUser, err := s.getSelfUser()
		if err != nil {
			s.logger.Error("Failed to get self user for reconstruction", zap.Error(err), zap.String("threadID", threadIDStr))
			s.conversationStore.AddToNegativeCache(threadIDStr)
			return nil
		}
		botDisplayName, err := s.getBotDisplayName()
		if err != nil {
			s.logger.Error("Failed to get bot display name for reconstruction", zap.Error(err))
			botDisplayName = defaultBotName
		}

		reconstructedData, reconstructedModelName, err := s.conversationStore.ReconstructAndCache(
			ctx, s.ses, evt.ChannelID, evt.ID, selfUser, botDisplayName, SanitizeOpenAIName, GetUserDisplayName,
		)
		if err != nil {
			s.logger.Error("Failed to reconstruct conversation", zap.Error(err), zap.String("threadID", threadIDStr))
			s.conversationStore.AddToNegativeCache(threadIDStr)
			return nil
		}
		if reconstructedData == nil {
			s.logger.Info("Thread not managed or initial message unparseable after reconstruction attempt, adding to negative cache.", zap.String("threadID", threadIDStr))
			s.conversationStore.AddToNegativeCache(threadIDStr)
			return nil
		}
		cachedData = reconstructedData
		modelToUse = reconstructedModelName
	}

	// Append the new user message
	authorDisplayName := GetUserDisplayName(evt.Author)
	newUserMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: evt.Content,
		Name:    SanitizeOpenAIName(authorDisplayName),
	}

	// Copy existing messages and add the new user message
	messages := append(cachedData.Messages, newUserMessage)

	// Manage Ongoing OpenAI Requests (Cancellation)
	requestCtx, cancelCurrentRequest := context.WithCancel(ctx)
	if existingCancel, loaded := s.ongoingRequests.LoadAndDelete(evt.ChannelID); loaded {
		if cancelFunc, ok := existingCancel.(context.CancelFunc); ok {
			s.logger.Info("Cancelling previous OpenAI request for thread", zap.String("threadID", threadIDStr))
			cancelFunc()
		}
	}
	s.ongoingRequests.Store(evt.ChannelID, cancelCurrentRequest)
	defer s.ongoingRequests.Delete(evt.ChannelID)
	defer cancelCurrentRequest()

	// Interact with OpenAI
	stopTyping := s.interactionManager.StartTypingIndicator(s.ses, evt.ChannelID)
	defer stopTyping()

	s.logger.Info("Sending request to OpenAI for thread message",
		zap.String("threadID", threadIDStr),
		zap.String("model", modelToUse),
		zap.Int("historyLength", len(messages)),
	)

	aiResponse, err := s.aiProvider.GetChatCompletion(requestCtx, modelToUse, messages)

	if errors.Is(requestCtx.Err(), context.Canceled) {
		s.logger.Info("OpenAI request was cancelled (likely by a newer message)", zap.String("threadID", threadIDStr))
		return nil
	}
	if err != nil {
		s.logger.Error("OpenAI completion failed for thread message", zap.Error(err), zap.String("threadID", threadIDStr))
		errMsgToThread := "Sorry, I encountered an error trying to process your message. Please try again."
		if sendErr := s.interactionManager.SendMessage(s.ses, evt.ChannelID, errMsgToThread); sendErr != nil {
			s.logger.Error("Failed to send error message to thread after OpenAI failure", zap.Error(sendErr), zap.String("threadID", threadIDStr))
		}
		return fmt.Errorf("OpenAI completion failed: %w", err)
	}

	if len(aiResponse.Choices) == 0 || aiResponse.Choices[0].Message.Content == "" {
		s.logger.Error("OpenAI returned no choices or empty message content", zap.String("threadID", threadIDStr))
		errMsgToThread := "Sorry, I received an empty response from the AI. Please try again."
		if sendErr := s.interactionManager.SendMessage(s.ses, evt.ChannelID, errMsgToThread); sendErr != nil {
			s.logger.Error("Failed to send error message to thread for empty AI response", zap.Error(sendErr), zap.String("threadID", threadIDStr))
		}
		return errors.New("OpenAI returned no choices")
	}

	aiMessageContent := aiResponse.Choices[0].Message.Content
	s.logger.Info("Received OpenAI response for thread message",
		zap.String("threadID", threadIDStr),
		zap.Int("promptTokens", aiResponse.Usage.PromptTokens),
		zap.Int("completionTokens", aiResponse.Usage.CompletionTokens),
	)

	// Send Response to Discord
	if err := s.interactionManager.SendMessage(s.ses, evt.ChannelID, aiMessageContent); err != nil {
		s.logger.Error("Failed to send AI response to thread", zap.Error(err), zap.String("threadID", threadIDStr))
	}

	// Update Cache
	botDisplayName, err := s.getBotDisplayName()
	if err != nil {
		s.logger.Error("Failed to get bot display name in HandleThreadMessage", zap.Error(err))
		botDisplayName = defaultBotName
	}

	aiMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: aiMessageContent,
		Name:    SanitizeOpenAIName(botDisplayName),
	}

	s.conversationStore.UpdateConversationWithNewMessages(threadIDStr, cachedData.Messages, newUserMessage, aiMessage, modelToUse)

	s.logger.Info("Successfully processed thread message and updated cache", zap.String("threadID", threadIDStr))
	return nil
}

// Helper methods for getting bot display name, etc.
func (s *Service) getBotDisplayName() (string, error) {
	botUser, err := s.ses.Me()
	if err != nil {
		s.logger.Error("Failed to get bot user for display name", zap.Error(err))
		return defaultBotName, err
	}
	return GetUserDisplayName(*botUser), nil
}

func (s *Service) getSelfUser() (*discord.User, error) {
	self, err := s.ses.Me()
	if err != nil {
		return nil, err
	}
	return self, nil
}
