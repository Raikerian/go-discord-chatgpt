package chat

import (
	"context"
	"strings"

	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
)

// OpenAITitleGenerator implements ThreadTitleGenerator using OpenAI API.
type OpenAITitleGenerator struct {
	client *openai.Client
	logger *zap.Logger
}

// NewOpenAITitleGenerator creates a new OpenAI-based title generator.
func NewOpenAITitleGenerator(client *openai.Client, logger *zap.Logger) ThreadTitleGenerator {
	return &OpenAITitleGenerator{
		client: client,
		logger: logger.Named("title_generator"),
	}
}

// GenerateTitle generates a thread title based on the conversation messages.
func (g *OpenAITitleGenerator) GenerateTitle(ctx context.Context, messages []openai.ChatCompletionMessage) (string, error) {
	// 1) System prompt to enforce style & length
	systemMsg := openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: "You are a helpful assistant that crafts a single-sentence thread title. " +
			"The title must be no longer than 60 characters, contain no quotes, " +
			"and be in the same language as the input.",
	}

	// 2) Build the final message sequence
	chatMessages := make([]openai.ChatCompletionMessage, 0, len(messages)+1)
	chatMessages = append(chatMessages, systemMsg)
	chatMessages = append(chatMessages, messages...)

	// 3) Call the Chat Completions endpoint
	resp, err := g.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model:            openai.GPT4Dot1Nano,
			Messages:         chatMessages,
			Temperature:      0.2, // low randomness
			MaxTokens:        20,  // short response
			TopP:             1.0, // full nucleus sampling
			FrequencyPenalty: 0.0, // no repetition penalty needed
			PresencePenalty:  0.0,
			Stop:             []string{"\n"}, // end at first newline
		},
	)
	if err != nil {
		g.logger.Warn("Failed to generate title via OpenAI", zap.Error(err))

		return "", err
	}

	if len(resp.Choices) == 0 {
		g.logger.Warn("OpenAI returned no choices for title generation")

		return "", nil
	}

	// 4) Clean up and return the title
	title := strings.TrimSpace(resp.Choices[0].Message.Content)
	g.logger.Debug("Generated thread title", zap.String("title", title))

	return title, nil
}
