package commands

import (
	"context"
	"errors"
	"fmt"
	"strings" // Added for message splitting
	"time"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"github.com/sashabaranov/go-openai" // Added for OpenAI client
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/Raikerian/go-discord-chatgpt/internal/gpt" // Added for message cache
)

const (
	discordMaxMessageLength                  = 2000 // Define Discord's max message length
	gptDiscordTypingIndicatorCooldownSeconds = 10
)

// ChatCommand handles the /chat command logic.
// It facilitates interaction with an AI model within a dedicated Discord thread.
type ChatCommand struct {
	logger        *zap.Logger
	cfg           *config.Config     // Application configuration
	openaiClient  *openai.Client     // OpenAI API client
	messagesCache *gpt.MessagesCache // Cache for message history
}

// NewChatCommand creates a new ChatCommand.
// It requires a logger, config, OpenAI client, and message cache.
func NewChatCommand(logger *zap.Logger, cfg *config.Config, openaiClient *openai.Client, messagesCache *gpt.MessagesCache) Command {
	return &ChatCommand{
		logger:        logger.Named("chat_command"),
		cfg:           cfg,
		openaiClient:  openaiClient,
		messagesCache: messagesCache,
	}
}

// Name returns the name of the command.
func (c *ChatCommand) Name() string {
	return "chat"
}

// Description returns the description of the command.
func (c *ChatCommand) Description() string {
	return "Starts a chat session with ChatGPT."
}

// Options returns the command options for the /chat command.
// It includes a required "message" option and an optional "model" option
// if AI models are configured.
func (c *ChatCommand) Options() []discord.CommandOption {
	baseOptions := []discord.CommandOption{
		&discord.StringOption{
			OptionName:  "message",
			Description: "Your message to ChatGPT",
			Required:    true,
		},
	}

	if c.cfg != nil && len(c.cfg.OpenAI.Models) > 0 {
		modelChoices := make([]discord.StringChoice, len(c.cfg.OpenAI.Models))
		for i, modelName := range c.cfg.OpenAI.Models {
			modelChoices[i] = discord.StringChoice{
				Name:  modelName,
				Value: modelName,
			}
		}

		baseOptions = append(baseOptions, &discord.StringOption{
			OptionName:  "model",
			Description: "Specific AI model to use (optional, defaults to first configured model)",
			Required:    false,
			Choices:     modelChoices,
		})
	}
	return baseOptions
}

// makeThreadName generates a suitable name for a Discord thread based on the user and prompt.
// It truncates the prompt part if the total length exceeds maxLength.
func makeThreadName(username, prompt string, maxLength int) string {
	prefix := fmt.Sprintf("Chat with %s: ", username)
	if len(prompt) == 0 {
		prompt = "New Chat"
	}

	maxPromptLen := maxLength - len(prefix)

	if maxPromptLen <= 0 {
		if len(prefix) > maxLength {
			if maxLength <= 3 {
				return prefix[:maxLength]
			}
			return prefix[:maxLength-3] + "..."
		}
		return prefix
	}

	var truncatedPrompt string
	if len(prompt) > maxPromptLen {
		if maxPromptLen <= 3 {
			truncatedPrompt = prompt[:maxPromptLen]
		} else {
			truncatedPrompt = prompt[:maxPromptLen-3] + "..."
		}
	} else {
		truncatedPrompt = prompt
	}

	name := prefix + truncatedPrompt
	if len(name) > maxLength {
		return name[:maxLength-3] + "..."
	}
	return name
}

// Execute handles the execution of the /chat command.
func (c *ChatCommand) Execute(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error {
	c.logger.Info("Chat command execution started",
		zap.String("user", e.Member.User.Username),
		zap.String("userID", e.Member.User.ID.String()),
	)

	// 1. Parse options
	var userPrompt, modelOption string
	for _, opt := range data.Options {
		switch opt.Name {
		case "message":
			userPrompt = opt.String()
		case "model":
			modelOption = opt.String()
		}
	}

	// 2. Validate prompt
	if userPrompt == "" {
		c.logger.Warn("User prompt is empty")
		errMsg := "Your message prompt cannot be empty."
		resp := api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString(errMsg),
				Flags:   discord.EphemeralMessage,
			},
		}
		if err := s.RespondInteraction(e.ID, e.Token, resp); err != nil {
			c.logger.Error("Failed to send ephemeral error for empty prompt", zap.Error(err))
		}
		return errors.New("prompt is empty")
	}

	// 3. Validate model configuration and determine model to use
	var modelToUse string
	if len(c.cfg.OpenAI.Models) == 0 {
		c.logger.Error("No OpenAI models configured")
		errMsg := "Error: No AI models are configured. Please contact an administrator."
		resp := api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Content: option.NewNullableString(errMsg),
				Flags:   discord.EphemeralMessage,
			},
		}
		if err := s.RespondInteraction(e.ID, e.Token, resp); err != nil {
			c.logger.Error("Failed to send ephemeral error for no models configured", zap.Error(err))
		}
		return errors.New("no openai models configured")
	}

	if modelOption != "" {
		isValidModel := false
		for _, configuredModel := range c.cfg.OpenAI.Models {
			if modelOption == configuredModel {
				modelToUse = modelOption
				isValidModel = true
				break
			}
		}
		if !isValidModel {
			c.logger.Warn("User specified an invalid model, defaulting.",
				zap.String("specifiedModel", modelOption),
				zap.Strings("availableModels", c.cfg.OpenAI.Models),
			)
			modelToUse = c.cfg.OpenAI.Models[0] // Default to the first configured model
		}
	} else {
		modelToUse = c.cfg.OpenAI.Models[0] // Default to the first configured model
	}
	c.logger.Info("Determined model for chat", zap.String("modelToUse", modelToUse))

	// 4. Prepare thread name and initial message content
	threadName := makeThreadName(e.Member.User.Username, userPrompt, 100)
	summaryMessage := fmt.Sprintf(
		"Starting new chat session with %s!\n**User:** %s\n**Prompt:** %s\n**Model:** %s\n\nFuture messages in this thread will continue the conversation.",
		e.Member.User.Username,
		e.Member.User.Mention(),
		userPrompt,
		modelToUse,
	)

	// 5. Send the initial interaction response (this will be the first message in the thread)
	initialResponseData := api.InteractionResponseData{
		Content: option.NewNullableString(summaryMessage),
		// No EphemeralMessage flag, so it's a public message
	}
	initialResponse := api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &initialResponseData,
	}

	if err := s.RespondInteraction(e.ID, e.Token, initialResponse); err != nil {
		c.logger.Error("Failed to send initial interaction response", zap.Error(err))
		// Attempt to send an ephemeral follow-up if the main response failed
		errMsg := "Sorry, I couldn't start the chat. Please try again."
		_, followUpErr := s.FollowUpInteraction(e.AppID, e.Token, api.InteractionResponseData{
			Content: option.NewNullableString(errMsg),
			Flags:   discord.EphemeralMessage,
		})
		if followUpErr != nil {
			c.logger.Error("Failed to send error follow-up for initial response failure", zap.Error(followUpErr))
		}
		return fmt.Errorf("failed to send initial interaction response: %w", err)
	}

	// 6. Fetch the original interaction response message we just sent.
	// This message's ID and ChannelID are needed to start the thread from it.
	originalMessage, err := s.Client.InteractionResponse(e.AppID, e.Token)
	if err != nil {
		c.logger.Error("Failed to get the initial interaction response message", zap.Error(err))
		// The user saw the summaryMessage, but we can't create a thread from it.
		// This is a tricky state. A follow-up message might be confusing or not possible if the token is invalid.
		// Log and return. The user has the initial message but no thread will be explicitly linked or created by the bot.
		return fmt.Errorf("failed to get interaction response message: %w", err)
	}
	c.logger.Info("Initial interaction response sent and fetched",
		zap.String("messageID", originalMessage.ID.String()),
		zap.String("channelID", originalMessage.ChannelID.String()),
	)

	// 7. Prepare thread creation data using the correct api.StartThreadData struct
	threadCreateAPIData := api.StartThreadData{
		Name:                threadName,                  // Name is a string
		AutoArchiveDuration: discord.ArchiveDuration(60), // AutoArchiveDuration is discord.ArchiveDuration
		// Type is omitted when starting a thread from a message, as per api.StartThreadWithMessage function body.
		// Invitable is also not applicable here as it's for threads without a message.
	}

	c.logger.Info("Attempting to create thread from message",
		zap.String("threadName", threadCreateAPIData.Name),
		zap.String("messageID", originalMessage.ID.String()),
		zap.String("channelID", originalMessage.ChannelID.String()),
	)

	// 8. Start the thread from the message
	// We use originalMessage.ChannelID as that's where the message lives.
	newThread, err := s.StartThreadWithMessage(originalMessage.ChannelID, originalMessage.ID, threadCreateAPIData)
	if err != nil {
		c.logger.Error("Failed to create thread from message", zap.Error(err))
		// The user saw the summaryMessage. Try to edit it to indicate thread creation failure.
		errMsgContent := fmt.Sprintf("%s\n\n**(Sorry, I couldn't create a discussion thread for this chat. Please try again or contact an administrator if the issue persists.)**", summaryMessage)
		_, editErr := s.EditInteractionResponse(e.AppID, e.Token, api.EditInteractionResponseData{
			Content: option.NewNullableString(errMsgContent),
		})
		if editErr != nil {
			c.logger.Error("Failed to edit interaction response to indicate thread creation failure", zap.Error(editErr))
		}
		return fmt.Errorf("failed to create thread from message: %w", err)
	}

	c.logger.Info("Thread created successfully from message",
		zap.String("threadID", newThread.ID.String()),
		zap.String("threadName", newThread.Name),
		zap.String("parentMessageID", originalMessage.ID.String()),
	)

	// 9. Send initial prompt to OpenAI
	c.logger.Info("Sending initial prompt to OpenAI",
		zap.String("userPrompt", userPrompt),
		zap.String("modelToUse", modelToUse),
		zap.String("threadID", newThread.ID.String()),
	)

	// Helper function to send typing indicator
	sendTyping := func() {
		if err := s.Client.Typing(newThread.ID); err != nil {
			c.logger.Warn("Failed to trigger typing indicator in new thread", zap.Error(err), zap.String("threadID", newThread.ID.String()))
		}
	}

	// Indicate bot is thinking immediately
	sendTyping()

	// Start a ticker for subsequent typing indicators
	typingTicker := time.NewTicker(gptDiscordTypingIndicatorCooldownSeconds * time.Second)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-typingTicker.C:
				sendTyping() // Call the helper function
			case <-done:
				typingTicker.Stop()
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
		// TODO: Consider adding other parameters like Temperature, MaxTokens, etc., from config or user options
	}

	aiResponse, err := c.openaiClient.CreateChatCompletion(ctx, aiRequest)
	if err != nil {
		c.logger.Error("Failed to get response from OpenAI", zap.Error(err), zap.String("threadID", newThread.ID.String()))
		// Try to send an error message to the thread
		errMsgToThread := "Sorry, I encountered an error trying to reach the AI. Please try again later."
		_, sendErr := s.Client.SendMessageComplex(newThread.ID, api.SendMessageData{Content: errMsgToThread})
		if sendErr != nil {
			c.logger.Error("Failed to send error message to thread after OpenAI failure", zap.Error(sendErr), zap.String("threadID", newThread.ID.String()))
		}
		return fmt.Errorf("failed to create chat completion: %w", err)
	}

	if len(aiResponse.Choices) == 0 || aiResponse.Choices[0].Message.Content == "" {
		c.logger.Warn("OpenAI returned an empty response", zap.Any("aiResponse", aiResponse), zap.String("threadID", newThread.ID.String()))
		// Try to send a message to the thread indicating no response
		noRespMsgToThread := "The AI didn't provide a response this time. You might want to try rephrasing your message."
		_, sendErr := s.Client.SendMessageComplex(newThread.ID, api.SendMessageData{Content: noRespMsgToThread})
		if sendErr != nil {
			c.logger.Error("Failed to send no-response message to thread", zap.Error(sendErr), zap.String("threadID", newThread.ID.String()))
		}
		return errors.New("openai returned empty response")
	}

	aiMessageContent := aiResponse.Choices[0].Message.Content
	c.logger.Info("Received response from OpenAI",
		zap.Int("promptTokens", aiResponse.Usage.PromptTokens),
		zap.Int("completionTokens", aiResponse.Usage.CompletionTokens),
		zap.Int("totalTokens", aiResponse.Usage.TotalTokens),
		zap.String("threadID", newThread.ID.String()),
	)

	// 10. Post AI's first answer to the thread, splitting if necessary
	if err := sendLongMessage(s, newThread.ID, aiMessageContent); err != nil {
		c.logger.Error("Failed to send AI response to thread", zap.Error(err), zap.String("threadID", newThread.ID.String()))
		// The user has the thread, but the AI message failed to send. This is not ideal.
		// We won't return an error to the interaction itself as the thread is created.
	}

	// Store the initial user prompt and AI response in the cache
	// The thread ID can serve as a unique key for the conversation history.
	if c.messagesCache != nil {
		history := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
			{Role: openai.ChatMessageRoleAssistant, Content: aiMessageContent},
		}
		// Wrap history in MessagesCacheData
		cacheData := &gpt.MessagesCacheData{
			Messages: history,
			Model:    modelToUse, // Store the model used for this interaction
			// SystemMessage, Temperature, TokenCount can be set if/when they are used
		}
		c.messagesCache.Add(newThread.ID.String(), cacheData)
		c.logger.Debug("Stored initial messages in cache", zap.String("threadID", newThread.ID.String()))
	}

	// The initial response is already the first message in the thread.
	// No further updates to the interaction response are needed on success.
	c.logger.Info("Chat command execution completed successfully", zap.String("threadID", newThread.ID.String()))
	return nil
}

// sendLongMessage sends a message to a Discord channel, splitting it into multiple messages
// if it exceeds discordMaxMessageLength.
func sendLongMessage(s *session.Session, channelID discord.ChannelID, content string) error {
	if len(content) <= discordMaxMessageLength {
		_, err := s.Client.SendMessageComplex(channelID, api.SendMessageData{Content: content})
		return err
	}

	var parts []string
	remainingContent := content
	for len(remainingContent) > 0 {
		if len(remainingContent) <= discordMaxMessageLength {
			parts = append(parts, remainingContent)
			break
		}

		// Find a good place to split (e.g., newline, space) to avoid breaking words/sentences awkwardly.
		splitAt := discordMaxMessageLength
		// Try to split at the last newline within the limit
		lastNewline := strings.LastIndex(remainingContent[:splitAt], "\\n")
		if lastNewline != -1 && lastNewline > 0 { // lastNewline > 0 to ensure we don't create empty messages if it starts with \\n
			splitAt = lastNewline
		} else {
			// If no newline, try to split at the last space within the limit
			lastSpace := strings.LastIndex(remainingContent[:splitAt], " ")
			if lastSpace != -1 && lastSpace > 0 { // lastSpace > 0 to ensure we don't create empty messages
				splitAt = lastSpace
			}
			// If no space or newline, we have to split mid-word (or the message is one giant word)
		}

		parts = append(parts, strings.TrimSpace(remainingContent[:splitAt]))
		remainingContent = strings.TrimSpace(remainingContent[splitAt:])
	}

	for i, part := range parts {
		if strings.TrimSpace(part) == "" { // Avoid sending empty messages
			continue
		}
		_, err := s.Client.SendMessageComplex(channelID, api.SendMessageData{Content: part})
		if err != nil {
			return fmt.Errorf("failed to send message part %d/%d: %w", i+1, len(parts), err)
		}
	}
	return nil
}
