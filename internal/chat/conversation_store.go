package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/gpt"
)

const (
	// discordMessageFetchLimit is the limit for fetching messages in a single API call during history reconstruction.
	discordMessageFetchLimit = 100
)

// ConversationStore defines the interface for storing, retrieving, and reconstructing conversation history.
type ConversationStore interface {
	GetConversation(threadID string) (data *gpt.MessagesCacheData, found bool)
	StoreInitialConversation(threadID string, userPrompt, aiResponse, model, userName, botName string, nameSanitizer func(string) string)
	UpdateConversationWithNewMessages(threadID string, existingMessages []openai.ChatCompletionMessage, newUserMessage, newAssistantMessage *openai.ChatCompletionMessage, modelName string)
	UpdateConversationMessages(threadID string, messages []openai.ChatCompletionMessage, model string)
	ReconstructAndCache(
		ctx context.Context,
		ses *session.Session,
		threadID discord.ChannelID,
		currentMessageIDToExclude discord.MessageID,
		selfUser *discord.User,
		botDisplayName string,
		nameSanitizer func(string) string,
		userDisplayNameResolver func(user *discord.User) string,
	) (cacheData *gpt.MessagesCacheData, modelName string, err error)
	AddToNegativeCache(threadID string)
	IsInNegativeCache(threadID string) bool
}

// NewConversationStore creates a new ConversationStore implementation with internal caches.
func NewConversationStore(
	logger *zap.Logger,
	messageCacheSize int,
	negativeThreadCacheSize int,
	summaryParser SummaryParser,
) ConversationStore {
	// Create caches directly using the new constructors
	messagesCache := gpt.NewMessagesCache(messageCacheSize)
	negativeThreadCache := gpt.NewNegativeThreadCache(negativeThreadCacheSize)

	return &cacheBasedConversationStore{
		logger:              logger.Named("conversation_store"),
		messagesCache:       &messagesCache,
		negativeThreadCache: &negativeThreadCache,
		summaryParser:       summaryParser,
	}
}

type cacheBasedConversationStore struct {
	logger              *zap.Logger
	messagesCache       *gpt.MessagesCache
	negativeThreadCache *gpt.NegativeThreadCache
	summaryParser       SummaryParser
}

// GetConversation retrieves a conversation from the cache.
func (cs *cacheBasedConversationStore) GetConversation(threadID string) (*gpt.MessagesCacheData, bool) {
	return cs.messagesCache.Get(threadID)
}

// StoreInitialConversation stores the initial user prompt and AI response in the message cache.
func (cs *cacheBasedConversationStore) StoreInitialConversation(threadID, userPrompt, aiResponse, model, userName, botName string, nameSanitizer func(string) string) {
	if cs.messagesCache != nil {
		history := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: userPrompt, Name: nameSanitizer(userName)},
			{Role: openai.ChatMessageRoleAssistant, Content: aiResponse, Name: nameSanitizer(botName)},
		}
		cacheData := &gpt.MessagesCacheData{
			Messages: history,
			Model:    model,
		}
		cs.messagesCache.Add(threadID, cacheData)
		cs.logger.Debug("Stored initial messages in cache", zap.String("threadID", threadID))
	}
}

// UpdateConversationWithNewMessages updates an existing conversation with new messages.
func (cs *cacheBasedConversationStore) UpdateConversationWithNewMessages(threadID string, existingMessages []openai.ChatCompletionMessage, newUserMessage, newAssistantMessage *openai.ChatCompletionMessage, modelName string) {
	updatedMessages := append(existingMessages, *newUserMessage, *newAssistantMessage)
	cacheData := &gpt.MessagesCacheData{
		Messages: updatedMessages,
		Model:    modelName,
	}
	cs.messagesCache.Add(threadID, cacheData)
	cs.logger.Debug("Updated conversation in cache", zap.String("threadID", threadID), zap.Int("messageCount", len(updatedMessages)))
}

// UpdateConversationMessages updates conversation with new messages (for immediate user message caching and AI response updates).
func (cs *cacheBasedConversationStore) UpdateConversationMessages(threadID string, messages []openai.ChatCompletionMessage, model string) {
	cacheData := &gpt.MessagesCacheData{
		Messages: messages,
		Model:    model,
	}
	cs.messagesCache.Add(threadID, cacheData)
	cs.logger.Debug("Updated conversation messages in cache", zap.String("threadID", threadID), zap.Int("messageCount", len(messages)))
}

// ReconstructAndCache reconstructs conversation history from Discord messages and caches it.
func (cs *cacheBasedConversationStore) ReconstructAndCache(
	ctx context.Context,
	ses *session.Session,
	threadID discord.ChannelID,
	currentMessageIDToExclude discord.MessageID,
	selfUser *discord.User,
	botDisplayName string,
	nameSanitizer func(string) string,
	userDisplayNameResolver func(user *discord.User) string,
) (cacheData *gpt.MessagesCacheData, modelName string, err error) {
	cs.logger.Info("Attempting to reconstruct conversation history for thread",
		zap.String("threadID", threadID.String()),
		zap.String("excludingMessageID", currentMessageIDToExclude.String()),
	)

	allDiscordMessages := make([]discord.Message, 0)
	var oldestMessageIDInBatch discord.MessageID = 0 // Start with 0 to fetch the latest messages first in the first call.

	for {
		var batch []discord.Message
		var fetchErr error

		if oldestMessageIDInBatch == 0 { // First fetch
			cs.logger.Debug("Fetching initial message batch for reconstruction", zap.String("threadID", threadID.String()), zap.Uint("limit", discordMessageFetchLimit))
			batch, fetchErr = ses.Messages(threadID, discordMessageFetchLimit)
		} else { // Subsequent fetches, get messages before the oldest one from the previous batch
			cs.logger.Debug("Fetching older message batch for reconstruction", zap.String("threadID", threadID.String()), zap.Stringer("beforeID", oldestMessageIDInBatch), zap.Uint("limit", discordMessageFetchLimit))
			batch, fetchErr = ses.MessagesBefore(threadID, oldestMessageIDInBatch, discordMessageFetchLimit)
		}

		if fetchErr != nil {
			cs.logger.Error("Failed to fetch messages during reconstruction", zap.Error(fetchErr), zap.String("threadID", threadID.String()))

			return nil, "", fmt.Errorf("failed to fetch messages for reconstruction: %w", fetchErr)
		}

		if len(batch) == 0 {
			cs.logger.Debug("Fetched empty batch, assuming end of messages for reconstruction", zap.String("threadID", threadID.String()))

			break
		}
		cs.logger.Debug("Fetched message batch", zap.Int("count", len(batch)), zap.String("threadID", threadID.String()))

		// Messages in batch are typically newest to oldest. We will reverse the whole list later.
		allDiscordMessages = append(allDiscordMessages, batch...)
		oldestMessageIDInBatch = batch[len(batch)-1].ID // The ID of the oldest message in the current batch.

		// If we fetched fewer messages than the limit, we've likely reached the beginning of the thread.
		if len(batch) < discordMessageFetchLimit {
			cs.logger.Debug("Fetched fewer messages than limit, assuming end of history.", zap.String("threadID", threadID.String()), zap.Int("fetchedCount", len(batch)))

			break
		}
	}
	cs.logger.Info("Fetched all messages for reconstruction", zap.Int("totalMessages", len(allDiscordMessages)), zap.String("threadID", threadID.String()))

	// Reverse messages to be in chronological order (oldest first)
	for i, j := 0, len(allDiscordMessages)-1; i < j; i, j = i+1, j-1 {
		allDiscordMessages[i], allDiscordMessages[j] = allDiscordMessages[j], allDiscordMessages[i]
	}

	if len(allDiscordMessages) == 0 {
		cs.logger.Warn("Thread is empty or unreadable during reconstruction", zap.String("threadID", threadID.String()))

		return nil, "", nil // Not an error, but signals not our thread or unreadable
	}

	summaryDiscordMessage := allDiscordMessages[0]
	if summaryDiscordMessage.Author.ID != selfUser.ID {
		cs.logger.Warn("First message in reconstructed thread not from bot, not a managed thread.",
			zap.String("threadID", threadID.String()),
			zap.String("firstMessageAuthorID", summaryDiscordMessage.Author.ID.String()),
			zap.String("botID", selfUser.ID.String()),
		)

		return nil, "", nil
	}

	// Use the summary parser to extract information from the initial message
	parsedUserPrompt, parsedModelName, initialUserDisplayName, err := cs.summaryParser.ParseInitialMessage(
		summaryDiscordMessage.Content,
		summaryDiscordMessage.ReferencedMessage,
		defaultInitialUserName,
		userDisplayNameResolver,
	)
	if err != nil {
		cs.logger.Warn("Failed to parse summary message", zap.Error(err), zap.String("threadID", threadID.String()))

		return nil, "", nil
	}

	history := []openai.ChatCompletionMessage{}
	history = append(history, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: parsedUserPrompt, Name: nameSanitizer(initialUserDisplayName)})

	for i := 1; i < len(allDiscordMessages); i++ {
		msg := allDiscordMessages[i]

		// Skip the current incoming message if its ID matches
		if msg.ID == currentMessageIDToExclude {
			cs.logger.Debug("Skipping current incoming message during history reconstruction",
				zap.String("threadID", threadID.String()),
				zap.String("messageID", msg.ID.String()))

			continue
		}

		var role string
		var name string
		if msg.Author.ID == selfUser.ID {
			role = openai.ChatMessageRoleAssistant
			name = nameSanitizer(botDisplayName)
		} else {
			role = openai.ChatMessageRoleUser
			messageAuthorDisplayName := userDisplayNameResolver(&msg.Author)
			name = nameSanitizer(messageAuthorDisplayName)
		}
		if strings.TrimSpace(msg.Content) == "" {
			cs.logger.Debug("Skipping empty message during history reconstruction", zap.String("threadID", threadID.String()), zap.String("messageID", msg.ID.String()))

			continue
		}
		history = append(history, openai.ChatCompletionMessage{Role: role, Content: msg.Content, Name: name})
	}
	cs.logger.Debug("Reconstructed message history", zap.Int("count", len(history)), zap.String("threadID", threadID.String()))

	reconstructedCacheData := &gpt.MessagesCacheData{
		Messages: history,
		Model:    parsedModelName,
	}
	cs.messagesCache.Add(threadID.String(), reconstructedCacheData)
	cs.logger.Info("Successfully reconstructed and cached conversation",
		zap.String("threadID", threadID.String()),
		zap.String("model", parsedModelName),
		zap.Int("historyLength", len(history)),
	)

	return reconstructedCacheData, parsedModelName, nil
}

// AddToNegativeCache adds a thread to the negative cache.
func (cs *cacheBasedConversationStore) AddToNegativeCache(threadID string) {
	cs.negativeThreadCache.Add(threadID)
	cs.logger.Debug("Added thread to negative cache", zap.String("threadID", threadID))
}

// IsInNegativeCache checks if a thread is in the negative cache.
func (cs *cacheBasedConversationStore) IsInNegativeCache(threadID string) bool {
	_, found := cs.negativeThreadCache.Get(threadID)

	return found
}
