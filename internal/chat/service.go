package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync" // Added for ongoingRequests
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
	gptDiscordTypingIndicatorCooldownSeconds = 10 // Existing constant
	// discordMessageFetchLimit is the limit for fetching messages in a single API call during history reconstruction.
	discordMessageFetchLimit = 100
)

// Service handles the core logic for chat interactions.
// It uses OpenAI and a message cache.
type Service struct {
	logger              *zap.Logger
	cfg                 *config.Config
	openaiClient        *openai.Client
	messagesCache       *gpt.MessagesCache
	negativeThreadCache *gpt.NegativeThreadCache
	ongoingRequests     sync.Map // map[discord.ChannelID]context.CancelFunc
}

// NewService creates a new chat Service.
func NewService(
	logger *zap.Logger,
	cfg *config.Config,
	openaiClient *openai.Client,
	messagesCache *gpt.MessagesCache,
	negativeThreadCache *gpt.NegativeThreadCache,
) *Service {
	return &Service{
		logger:              logger.Named("chat_service"),
		cfg:                 cfg,
		openaiClient:        openaiClient,
		messagesCache:       messagesCache,
		negativeThreadCache: negativeThreadCache,
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

// HandleThreadMessage handles messages sent in a thread that the bot is part of.
func (s *Service) HandleThreadMessage(ctx context.Context, ses *session.Session, evt *gateway.MessageCreateEvent) error {
	s.logger.Info("Handling thread message",
		zap.String("threadID", evt.ChannelID.String()),
		zap.String("authorID", evt.Author.ID.String()),
		zap.String("content", evt.Content),
	)
	threadIDStr := evt.ChannelID.String()

	// Negative Cache Check
	if _, found := s.negativeThreadCache.Get(threadIDStr); found {
		s.logger.Debug("Thread is in negative cache, ignoring message", zap.String("threadID", threadIDStr))
		return nil
	}

	var modelToUse string
	cachedData, found := s.messagesCache.Get(threadIDStr)

	if found {
		s.logger.Debug("Found conversation in primary cache", zap.String("threadID", threadIDStr))
		modelToUse = cachedData.Model
	} else {
		s.logger.Info("Conversation not in cache, attempting to reconstruct", zap.String("threadID", threadIDStr))
		reconstructedData, reconstructedModelName, err := s.reconstructAndCacheConversation(ctx, ses, evt.ChannelID, evt.ID)
		if err != nil {
			s.logger.Error("Failed to reconstruct conversation", zap.Error(err), zap.String("threadID", threadIDStr))
			s.negativeThreadCache.Add(threadIDStr)
			return nil
		}
		if reconstructedData == nil {
			s.logger.Info("Thread not managed or initial message unparseable after reconstruction attempt, adding to negative cache.", zap.String("threadID", threadIDStr))
			// Add to negative cache as the reconstruction determined it's not a managed thread or unparseable.
			s.negativeThreadCache.Add(threadIDStr)
			return nil
		}
		cachedData = reconstructedData
		modelToUse = reconstructedModelName
	}

	// Append the new user message
	newUserMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: evt.Content,
	}
	cachedData.Messages = append(cachedData.Messages, newUserMessage)

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
	defer cancelCurrentRequest() // Ensure this request's context is cancelled when done

	// Interact with OpenAI
	stopTyping := s.manageTypingIndicator(ses, evt.ChannelID)
	defer func() { stopTyping <- true }()

	aiRequest := openai.ChatCompletionRequest{
		Model:    modelToUse,
		Messages: cachedData.Messages,
	}

	s.logger.Info("Sending request to OpenAI for thread message",
		zap.String("threadID", threadIDStr),
		zap.String("model", modelToUse),
		zap.Int("historyLength", len(cachedData.Messages)),
	)

	aiResponse, err := s.openaiClient.CreateChatCompletion(requestCtx, aiRequest)

	if errors.Is(requestCtx.Err(), context.Canceled) {
		s.logger.Info("OpenAI request was cancelled (likely by a newer message)", zap.String("threadID", threadIDStr))
		return nil // Not an error to propagate, cancellation is an expected flow
	}
	if err != nil {
		s.logger.Error("OpenAI completion failed for thread message", zap.Error(err), zap.String("threadID", threadIDStr))
		errMsgToThread := "Sorry, I encountered an error trying to process your message. Please try again."
		if sendErr := SendLongMessage(ses, evt.ChannelID, errMsgToThread); sendErr != nil {
			s.logger.Error("Failed to send error message to thread after OpenAI failure", zap.Error(sendErr), zap.String("threadID", threadIDStr))
		}
		return fmt.Errorf("OpenAI completion failed: %w", err)
	}

	if len(aiResponse.Choices) == 0 || aiResponse.Choices[0].Message.Content == "" {
		s.logger.Error("OpenAI returned no choices or empty message content", zap.String("threadID", threadIDStr))
		errMsgToThread := "Sorry, I received an empty response from the AI. Please try again."
		if sendErr := SendLongMessage(ses, evt.ChannelID, errMsgToThread); sendErr != nil {
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
	if err := SendLongMessage(ses, evt.ChannelID, aiMessageContent); err != nil {
		s.logger.Error("Failed to send AI response to thread", zap.Error(err), zap.String("threadID", threadIDStr))
		// Don't return error, as AI call succeeded.
	}

	// Update Cache
	aiMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: aiMessageContent,
	}
	cachedData.Messages = append(cachedData.Messages, aiMessage)
	s.messagesCache.Add(threadIDStr, cachedData)

	s.logger.Info("Successfully processed thread message and updated cache", zap.String("threadID", threadIDStr))
	return nil
}

func (s *Service) reconstructAndCacheConversation(ctx context.Context, ses *session.Session, threadID discord.ChannelID, currentMessageIDToExclude discord.MessageID) (cacheData *gpt.MessagesCacheData, modelName string, err error) {
	s.logger.Info("Attempting to reconstruct conversation history for thread",
		zap.String("threadID", threadID.String()),
		zap.String("excludingMessageID", currentMessageIDToExclude.String()),
	)

	allDiscordMessages := make([]discord.Message, 0)
	var oldestMessageIDInBatch discord.MessageID = 0 // Start with 0 to fetch the latest messages first in the first call.

	for {
		var batch []discord.Message
		var fetchErr error

		if oldestMessageIDInBatch == 0 { // First fetch
			s.logger.Debug("Fetching initial message batch for reconstruction", zap.String("threadID", threadID.String()), zap.Uint("limit", discordMessageFetchLimit))
			batch, fetchErr = ses.Messages(threadID, discordMessageFetchLimit)
		} else { // Subsequent fetches, get messages before the oldest one from the previous batch
			s.logger.Debug("Fetching older message batch for reconstruction", zap.String("threadID", threadID.String()), zap.Stringer("beforeID", oldestMessageIDInBatch), zap.Uint("limit", discordMessageFetchLimit))
			batch, fetchErr = ses.MessagesBefore(threadID, oldestMessageIDInBatch, discordMessageFetchLimit)
		}

		if fetchErr != nil {
			s.logger.Error("Failed to fetch messages during reconstruction", zap.Error(fetchErr), zap.String("threadID", threadID.String()))
			return nil, "", fmt.Errorf("failed to fetch messages for reconstruction: %w", fetchErr)
		}

		if len(batch) == 0 {
			s.logger.Debug("Fetched empty batch, assuming end of messages for reconstruction", zap.String("threadID", threadID.String()))
			break
		}
		s.logger.Debug("Fetched message batch", zap.Int("count", len(batch)), zap.String("threadID", threadID.String()))

		// Messages in batch are typically newest to oldest. We will reverse the whole list later.
		allDiscordMessages = append(allDiscordMessages, batch...)
		oldestMessageIDInBatch = batch[len(batch)-1].ID // The ID of the oldest message in the current batch.

		// If we fetched fewer messages than the limit, we've likely reached the beginning of the thread.
		if len(batch) < discordMessageFetchLimit {
			s.logger.Debug("Fetched fewer messages than limit, assuming end of history.", zap.String("threadID", threadID.String()), zap.Int("fetchedCount", len(batch)))
			break
		}
	}
	s.logger.Info("Fetched all messages for reconstruction", zap.Int("totalMessages", len(allDiscordMessages)), zap.String("threadID", threadID.String()))

	// Reverse messages to be in chronological order (oldest first)
	for i, j := 0, len(allDiscordMessages)-1; i < j; i, j = i+1, j-1 {
		allDiscordMessages[i], allDiscordMessages[j] = allDiscordMessages[j], allDiscordMessages[i]
	}

	if len(allDiscordMessages) == 0 {
		s.logger.Warn("Thread is empty or unreadable during reconstruction", zap.String("threadID", threadID.String()))
		return nil, "", nil // Not an error, but signals not our thread or unreadable
	}

	summaryDiscordMessage := allDiscordMessages[0]
	selfUser, err := ses.Me()
	if err != nil {
		s.logger.Error("Failed to get self user for reconstruction validation", zap.Error(err), zap.String("threadID", threadID.String()))
		return nil, "", fmt.Errorf("failed to get self user: %w", err)
	}

	if summaryDiscordMessage.Author.ID != selfUser.ID {
		s.logger.Warn("First message in reconstructed thread not from bot, not a managed thread.",
			zap.String("threadID", threadID.String()),
			zap.String("firstMessageAuthorID", summaryDiscordMessage.Author.ID.String()),
			zap.String("botID", selfUser.ID.String()),
		)
		return nil, "", nil
	}

	content := summaryDiscordMessage.Content
	if content == "" && summaryDiscordMessage.ReferencedMessage != nil {
		s.logger.Debug("Summary message content is empty, using referenced message content", zap.String("threadID", threadID.String()))
		content = summaryDiscordMessage.ReferencedMessage.Content
	}

	promptMarker := "**Prompt:** "
	modelMarker := "\n**Model:** " // Adjusted to expect newline before **Model:**
	endOfModelMarker := "\n\nFuture messages"

	promptStartIndex := strings.Index(content, promptMarker)
	if promptStartIndex == -1 {
		s.logger.Warn("Could not find 'Prompt:' marker in summary message", zap.String("threadID", threadID.String()), zap.String("content", content))
		return nil, "", nil
	}
	// Actual start of the prompt text
	actualPromptStartIndex := promptStartIndex + len(promptMarker)

	// Find the start of the model line, which should be after the prompt text
	modelLineStartIndex := strings.Index(content[actualPromptStartIndex:], modelMarker)
	if modelLineStartIndex == -1 {
		s.logger.Warn("Could not find 'Model:' marker after prompt in summary message", zap.String("threadID", threadID.String()), zap.String("substringSearched", content[actualPromptStartIndex:]))
		return nil, "", nil
	}
	// modelLineStartIndex is relative to the substring content[actualPromptStartIndex:]. Adjust to be relative to content.
	modelLineAbsoluteStartIndex := actualPromptStartIndex + modelLineStartIndex

	// The prompt text is between actualPromptStartIndex and modelLineAbsoluteStartIndex
	parsedUserPrompt := strings.TrimSpace(content[actualPromptStartIndex:modelLineAbsoluteStartIndex])

	// Actual start of the model name text
	actualModelNameStartIndex := modelLineAbsoluteStartIndex + len(modelMarker)

	// Find the end of the model name (start of "Future messages" or end of line/string)
	endOfModelNameIndex := strings.Index(content[actualModelNameStartIndex:], endOfModelMarker)
	parsedModelName := ""
	if endOfModelNameIndex != -1 {
		// endOfModelNameIndex is relative to content[actualModelNameStartIndex:]. Adjust to be relative to content.
		parsedModelName = strings.TrimSpace(content[actualModelNameStartIndex : actualModelNameStartIndex+endOfModelNameIndex])
	} else {
		// Fallback: if "Future messages" part is missing, try to read until end of line or string
		nextLineBreakIndex := strings.Index(content[actualModelNameStartIndex:], "\n")
		if nextLineBreakIndex != -1 {
			parsedModelName = strings.TrimSpace(content[actualModelNameStartIndex : actualModelNameStartIndex+nextLineBreakIndex])
		} else {
			parsedModelName = strings.TrimSpace(content[actualModelNameStartIndex:]) // Take rest of string
		}
	}

	if parsedUserPrompt == "" || parsedModelName == "" {
		s.logger.Warn("Failed to parse user prompt or model name from summary message",
			zap.String("threadID", threadID.String()),
			zap.String("parsedPrompt", parsedUserPrompt),
			zap.String("parsedModel", parsedModelName),
			zap.String("summaryContent", content),
		)
		return nil, "", nil
	}
	s.logger.Info("Successfully parsed summary message",
		zap.String("threadID", threadID.String()),
		zap.String("parsedUserPrompt", parsedUserPrompt),
		zap.String("parsedModelName", parsedModelName),
	)

	history := []openai.ChatCompletionMessage{}
	history = append(history, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: parsedUserPrompt}) // Name for the first user prompt might need to be derived if possible, or a generic one.

	for i := 1; i < len(allDiscordMessages); i++ {
		msg := allDiscordMessages[i]

		// Skip the current incoming message if its ID matches
		if msg.ID == currentMessageIDToExclude {
			s.logger.Debug("Skipping current incoming message during history reconstruction",
				zap.String("threadID", threadID.String()),
				zap.String("messageID", msg.ID.String()))
			continue
		}

		var role string
		if msg.Author.ID == selfUser.ID {
			role = openai.ChatMessageRoleAssistant
		} else {
			role = openai.ChatMessageRoleUser
		}
		if strings.TrimSpace(msg.Content) == "" {
			s.logger.Debug("Skipping empty message during history reconstruction", zap.String("threadID", threadID.String()), zap.String("messageID", msg.ID.String()))
			continue
		}
		history = append(history, openai.ChatCompletionMessage{Role: role, Content: msg.Content})
	}
	s.logger.Debug("Reconstructed message history", zap.Int("count", len(history)), zap.String("threadID", threadID.String()))

	reconstructedCacheData := &gpt.MessagesCacheData{
		Messages: history,
		Model:    parsedModelName,
	}
	s.messagesCache.Add(threadID.String(), reconstructedCacheData)
	s.logger.Info("Successfully reconstructed and cached conversation",
		zap.String("threadID", threadID.String()),
		zap.String("model", parsedModelName),
		zap.Int("historyLength", len(history)),
	)

	return reconstructedCacheData, parsedModelName, nil
}
