package chat

import (
	"errors"
	"strings"

	"github.com/diamondburned/arikawa/v3/discord"
	"go.uber.org/zap"
)

// SummaryParser defines the interface for parsing the initial summary message of a chat thread.
type SummaryParser interface {
	ParseInitialMessage(
		content string,
		referencedMessage *discord.Message,
		defaultInitialUserName string,
		userDisplayNameResolver func(user discord.User) string,
	) (parsedUserPrompt, parsedModelName, initialUserName string, err error)
}

// NewSummaryParser creates a new SummaryParser.
func NewSummaryParser(logger *zap.Logger) SummaryParser {
	return &chatSummaryParser{
		logger: logger.Named("summary_parser"),
	}
}

type chatSummaryParser struct {
	logger *zap.Logger
}

// ParseInitialMessage parses the bot's summary message format to extract conversation metadata.
func (csp *chatSummaryParser) ParseInitialMessage(
	content string,
	referencedMessage *discord.Message,
	defaultInitialUserName string,
	userDisplayNameResolver func(user discord.User) string,
) (parsedUserPrompt, parsedModelName, initialUserName string, err error) {
	initialUserDisplayName := defaultInitialUserName

	// Check if we need to use referenced message content
	if content == "" && referencedMessage != nil {
		csp.logger.Debug("Summary message content is empty, using referenced message content")
		content = referencedMessage.Content
		if referencedMessage.Interaction != nil && referencedMessage.Interaction.User.ID.IsValid() {
			originalInteractionUser := referencedMessage.Interaction.User
			initialUserDisplayName = userDisplayNameResolver(originalInteractionUser)
		}
	}

	// Parse the summary message format
	promptMarker := "**Prompt:** "
	modelMarker := "\n**Model:** " // Adjusted to expect newline before **Model:**
	endOfModelMarker := "\n\nFuture messages"

	promptStartIndex := strings.Index(content, promptMarker)
	if promptStartIndex == -1 {
		csp.logger.Warn("Could not find 'Prompt:' marker in summary message", zap.String("content", content))

		return "", "", "", errors.New("could not find prompt marker")
	}
	// Actual start of the prompt text
	actualPromptStartIndex := promptStartIndex + len(promptMarker)

	// Find the start of the model line, which should be after the prompt text
	modelLineStartIndex := strings.Index(content[actualPromptStartIndex:], modelMarker)
	if modelLineStartIndex == -1 {
		csp.logger.Warn("Could not find 'Model:' marker after prompt in summary message", zap.String("substringSearched", content[actualPromptStartIndex:]))

		return "", "", "", errors.New("could not find model marker")
	}
	// modelLineStartIndex is relative to the substring content[actualPromptStartIndex:]. Adjust to be relative to content.
	modelLineAbsoluteStartIndex := actualPromptStartIndex + modelLineStartIndex

	// The prompt text is between actualPromptStartIndex and modelLineAbsoluteStartIndex
	parsedUserPrompt = strings.TrimSpace(content[actualPromptStartIndex:modelLineAbsoluteStartIndex])

	// Actual start of the model name text
	actualModelNameStartIndex := modelLineAbsoluteStartIndex + len(modelMarker)

	// Find the end of the model name (start of "Future messages" or end of line/string)
	endOfModelNameIndex := strings.Index(content[actualModelNameStartIndex:], endOfModelMarker)
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
		csp.logger.Warn("Failed to parse user prompt or model name from summary message",
			zap.String("parsedPrompt", parsedUserPrompt),
			zap.String("parsedModel", parsedModelName),
			zap.String("summaryContent", content),
		)

		return "", "", "", errors.New("failed to parse prompt or model from summary")
	}

	csp.logger.Info("Successfully parsed summary message",
		zap.String("parsedUserPrompt", parsedUserPrompt),
		zap.String("parsedModelName", parsedModelName),
		zap.String("initialUserName", initialUserDisplayName),
	)

	return parsedUserPrompt, parsedModelName, initialUserDisplayName, nil
}
