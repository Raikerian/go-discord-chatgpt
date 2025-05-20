package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
)

// ChatCommand handles the /chat command logic.
// It facilitates interaction with an AI model within a dedicated Discord thread.
type ChatCommand struct {
	logger *zap.Logger
	cfg    *config.Config // Application configuration
}

// NewChatCommand creates a new ChatCommand.
// It requires a logger for logging and config for accessing OpenAI models.
func NewChatCommand(logger *zap.Logger, cfg *config.Config) Command {
	return &ChatCommand{
		logger: logger.Named("chat_command"),
		cfg:    cfg,
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
// 1. Parses user input (prompt and optional model).
// 2. Validates input and configuration, sending ephemeral errors if needed.
// 3. Sends an initial public interaction response with chat details; this message becomes the start of the thread.
// 4. Fetches the message object of this initial response to get its ID and ChannelID.
// 5. Creates a new Discord thread associated with the initial response message.
// 6. If thread creation fails, attempts to edit the initial response to notify the user.
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

	// The initial response is already the first message in the thread.
	// No further updates to the interaction response are needed on success.
	c.logger.Info("Chat command execution completed successfully", zap.String("threadID", newThread.ID.String()))
	return nil
}
