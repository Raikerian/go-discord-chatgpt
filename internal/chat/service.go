package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/sashabaranov/go-openai"

	"go.uber.org/zap"
)

// ThreadTitleGenerator defines the interface for generating thread titles based on conversation context.
type ThreadTitleGenerator interface {
	GenerateTitle(ctx context.Context, messages []openai.ChatCompletionMessage) (string, error)
}

// Service orchestrates chat interactions by coordinating various specialized services.
type Service struct {
	logger *zap.Logger
	cfg    *config.Config
	ses    *session.Session

	interactionManager DiscordInteractionManager
	aiProvider         AIProvider
	conversationStore  ConversationStore
	modelSelector      ModelSelector
	titleGenerator     ThreadTitleGenerator

	// ongoingRequests stores cancel functions for ongoing OpenAI requests per thread.
	// key: discord.ChannelID, value: context.CancelFunc
	ongoingRequests sync.Map

	// threadMutexes ensures sequential processing per thread for cache consistency.
	// key: discord.ChannelID, value: *sync.Mutex
	threadMutexes sync.Map
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
	titleGenerator ThreadTitleGenerator,
) *Service {
	return &Service{
		logger:             logger.Named("chat_service_orchestrator"),
		cfg:                cfg,
		ses:                ses,
		interactionManager: interactionManager,
		aiProvider:         aiProvider,
		conversationStore:  conversationStore,
		modelSelector:      modelSelector,
		titleGenerator:     titleGenerator,
	}
}

// getOrCreateThreadMutex returns a mutex for the given thread to ensure sequential processing.
func (s *Service) getOrCreateThreadMutex(channelID discord.ChannelID) *sync.Mutex {
	mutex, _ := s.threadMutexes.LoadOrStore(channelID, &sync.Mutex{})

	return mutex.(*sync.Mutex)
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

	userDisplayName := GetUserDisplayName(&e.Member.User)
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

		return fmt.Errorf("failed to send AI response to Discord: %w", err)
	}

	// Generate thread title asynchronously after successful AI response
	titleCtx, titleCancel := context.WithTimeout(context.Background(), 10*time.Second)
	go func() {
		defer titleCancel()
		s.generateAndUpdateThreadTitle(titleCtx, newThread.ID, messages, &aiResponse.Choices[0].Message)
	}()

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

	threadMutex := s.getOrCreateThreadMutex(evt.ChannelID)

	// 1. IMMEDIATE CANCELLATION (no lock needed for sync.Map)
	if existingCancel, loaded := s.ongoingRequests.LoadAndDelete(evt.ChannelID); loaded {
		if cancelFunc, ok := existingCancel.(context.CancelFunc); ok {
			s.logger.Info("Canceling previous OpenAI request for thread",
				zap.String("threadID", evt.ChannelID.String()))
			cancelFunc() // Previous request should return quickly
		}
	}

	// 2. ACQUIRE PROCESSING LOCK for cache consistency
	threadMutex.Lock()
	defer threadMutex.Unlock()

	// 3. SET UP NEW REQUEST CONTEXT
	requestCtx, cancel := context.WithCancel(ctx)
	s.ongoingRequests.Store(evt.ChannelID, cancel)

	defer func() {
		s.ongoingRequests.Delete(evt.ChannelID)
		cancel()
	}()

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
			requestCtx, s.ses, evt.ChannelID, evt.ID, selfUser, botDisplayName, SanitizeOpenAIName, GetUserDisplayName,
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

	// 4. IMMEDIATELY add user message to cache (after reconstruction if needed)
	authorDisplayName := GetUserDisplayName(&evt.Author)
	newUserMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: evt.Content,
		Name:    SanitizeOpenAIName(authorDisplayName),
	}

	// Copy existing messages and add the new user message
	messages := append(cachedData.Messages, newUserMessage)

	// Update cache with user message immediately
	s.conversationStore.UpdateConversationMessages(threadIDStr, messages, modelToUse)
	s.logger.Debug("User message added to cache immediately",
		zap.String("threadID", threadIDStr),
		zap.String("userMessage", evt.Content),
		zap.Int("totalMessages", len(messages)))

	// 5. Send to OpenAI (this should return quickly if canceled)
	stopTyping := s.interactionManager.StartTypingIndicator(s.ses, evt.ChannelID)
	defer stopTyping()

	s.logger.Info("Sending request to OpenAI for thread message",
		zap.String("threadID", threadIDStr),
		zap.String("model", modelToUse),
		zap.Int("historyLength", len(messages)),
	)

	aiResponse, err := s.aiProvider.GetChatCompletion(requestCtx, modelToUse, messages)

	// Handle cancellation
	if errors.Is(requestCtx.Err(), context.Canceled) {
		s.logger.Info("OpenAI request was canceled, user message preserved in cache",
			zap.String("threadID", threadIDStr))

		return nil // User message already cached, lock will be released by defer
	}

	// Handle OpenAI errors (user message already cached)
	if err != nil {
		s.logger.Error("OpenAI completion failed for thread message", zap.Error(err))
		// Send error to Discord but preserve user message in cache
		errMsg := "Sorry, I encountered an error. Please try again."
		if sendErr := s.interactionManager.SendMessage(s.ses, evt.ChannelID, errMsg); sendErr != nil {
			s.logger.Error("Failed to send error message", zap.Error(sendErr))
		}

		return fmt.Errorf("OpenAI completion failed: %w", err) // User message preserved
	}

	// 6. Process successful response
	if len(aiResponse.Choices) == 0 || aiResponse.Choices[0].Message.Content == "" {
		s.logger.Error("OpenAI returned no choices or empty message content", zap.String("threadID", threadIDStr))
		errMsgToThread := "Sorry, I received an empty response from the AI. Please try again."
		if sendErr := s.interactionManager.SendMessage(s.ses, evt.ChannelID, errMsgToThread); sendErr != nil {
			s.logger.Error("Failed to send error message to thread for empty AI response", zap.Error(sendErr), zap.String("threadID", threadIDStr))
		}

		return errors.New("OpenAI returned no choices") // User message preserved
	}

	aiMessageContent := aiResponse.Choices[0].Message.Content
	s.logger.Info("Received OpenAI response for thread message",
		zap.String("threadID", threadIDStr),
		zap.Int("promptTokens", aiResponse.Usage.PromptTokens),
		zap.Int("completionTokens", aiResponse.Usage.CompletionTokens),
	)

	// Send response to Discord
	if sendErr := s.interactionManager.SendMessage(s.ses, evt.ChannelID, aiMessageContent); sendErr != nil {
		s.logger.Error("Failed to send AI response to thread", zap.Error(sendErr), zap.String("threadID", threadIDStr))

		return fmt.Errorf("failed to send AI response to Discord: %w", sendErr)
	}

	// 7. Add AI response to cache (with validation)
	currentCachedData, found := s.conversationStore.GetConversation(threadIDStr)
	if !found {
		s.logger.Warn("Conversation cache was evicted during OpenAI request, AI response not cached",
			zap.String("threadID", threadIDStr))

		return nil // User message already cached, graceful degradation
	}

	botDisplayName, err := s.getBotDisplayName()
	if err != nil {
		s.logger.Error("Failed to get bot display name", zap.Error(err))
		botDisplayName = "Assistant"
	}

	aiMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: aiMessageContent,
		Name:    SanitizeOpenAIName(botDisplayName),
	}

	finalMessages := append(currentCachedData.Messages, aiMessage)
	s.conversationStore.UpdateConversationMessages(threadIDStr, finalMessages, modelToUse)
	s.logger.Info("AI response added to cache",
		zap.String("threadID", threadIDStr),
		zap.Int("finalMessageCount", len(finalMessages)))

	return nil
}

// generateAndUpdateThreadTitle generates a title for the thread based on the conversation
// and updates the Discord thread name asynchronously.
func (s *Service) generateAndUpdateThreadTitle(ctx context.Context, threadID discord.ChannelID, userMessages []openai.ChatCompletionMessage, aiResponse *openai.ChatCompletionMessage) {
	// Build the complete conversation for title generation
	allMessages := append(userMessages, *aiResponse)

	title, err := s.titleGenerator.GenerateTitle(ctx, allMessages)
	if err != nil {
		s.logger.Warn("Failed to generate thread title",
			zap.Error(err),
			zap.String("threadID", threadID.String()))

		return
	}

	// Clean up and validate the title
	title = strings.TrimSpace(title)
	if title == "" {
		s.logger.Warn("Generated thread title is empty",
			zap.String("threadID", threadID.String()))

		return
	}

	// Update the Discord thread name using arikawa v3 API
	err = s.ses.ModifyChannel(threadID, api.ModifyChannelData{
		Name: title,
	})
	if err != nil {
		s.logger.Warn("Failed to update thread name",
			zap.Error(err),
			zap.String("threadID", threadID.String()),
			zap.String("title", title))

		return
	}

	s.logger.Info("Successfully updated thread title",
		zap.String("threadID", threadID.String()),
		zap.String("title", title))
}

// Helper methods for getting bot display name, etc.
func (s *Service) getBotDisplayName() (string, error) {
	botUser, err := s.ses.Me()
	if err != nil {
		s.logger.Error("Failed to get bot user for display name", zap.Error(err))

		return defaultBotName, err
	}

	return GetUserDisplayName(botUser), nil
}

func (s *Service) getSelfUser() (*discord.User, error) {
	self, err := s.ses.Me()
	if err != nil {
		return nil, err
	}

	return self, nil
}
