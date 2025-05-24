package chat

import (
	"context"
	"errors"

	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
)

// AIProvider defines the interface for interacting with an AI chat completion service.
type AIProvider interface {
	GetChatCompletion(ctx context.Context, model string, messages []openai.ChatCompletionMessage) (*openai.ChatCompletionResponse, error)
}

// NewOpenAIProvider creates a new OpenAI-based AIProvider implementation.
func NewOpenAIProvider(logger *zap.Logger, cfg *config.Config, client *openai.Client) AIProvider {
	return &openAIProvider{
		logger: logger.Named("openai_provider"),
		cfg:    cfg,
		client: client,
	}
}

type openAIProvider struct {
	logger *zap.Logger
	client *openai.Client
	cfg    *config.Config
}

// GetChatCompletion sends a chat completion request to OpenAI and returns the response.
func (oai *openAIProvider) GetChatCompletion(ctx context.Context, model string, messages []openai.ChatCompletionMessage) (*openai.ChatCompletionResponse, error) {
	oai.logger.Info("Sending request to OpenAI",
		zap.String("model", model),
		zap.Int("messageCount", len(messages)),
	)

	aiRequest := openai.ChatCompletionRequest{
		Model:    model,
		Messages: messages,
	}

	aiResponse, err := oai.client.CreateChatCompletion(ctx, aiRequest)
	if err != nil {
		oai.logger.Error("Failed to get response from OpenAI", zap.Error(err))

		return nil, err
	}

	if len(aiResponse.Choices) == 0 || aiResponse.Choices[0].Message.Content == "" {
		oai.logger.Warn("OpenAI returned an empty response", zap.Any("aiResponse", aiResponse))

		return nil, errors.New("OpenAI returned empty response")
	}

	oai.logger.Info("Received response from OpenAI",
		zap.Int("promptTokens", aiResponse.Usage.PromptTokens),
		zap.Int("completionTokens", aiResponse.Usage.CompletionTokens),
		zap.Int("totalTokens", aiResponse.Usage.TotalTokens),
	)

	return &aiResponse, nil
}
