// Package chat provides modular chat service components for handling Discord chat interactions with AI.
package chat

import (
	"context"
	"errors"

	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	pkgopenai "github.com/Raikerian/go-discord-chatgpt/pkg/openai"
)

// AIProvider defines the interface for interacting with an AI chat completion service.
type AIProvider interface {
	GetChatCompletion(ctx context.Context, model string, messages []openai.ChatCompletionMessage) (*openai.ChatCompletionResponse, error)
}

// NewOpenAIProvider creates a new OpenAI-based AIProvider implementation.
func NewOpenAIProvider(logger *zap.Logger, cfg *config.Config, client *openai.Client, pricingService pkgopenai.PricingService) AIProvider {
	return &openAIProvider{
		logger:         logger.Named("openai_provider"),
		cfg:            cfg,
		client:         client,
		pricingService: pricingService,
	}
}

type openAIProvider struct {
	logger         *zap.Logger
	client         *openai.Client
	cfg            *config.Config
	pricingService pkgopenai.PricingService
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

	// Calculate and log cost information
	cost, costErr := oai.pricingService.CalculateTokenCost(
		model,
		aiResponse.Usage.PromptTokens,
		aiResponse.Usage.CompletionTokens,
	)

	logFields := []zap.Field{
		zap.Int("promptTokens", aiResponse.Usage.PromptTokens),
		zap.Int("completionTokens", aiResponse.Usage.CompletionTokens),
		zap.Int("totalTokens", aiResponse.Usage.TotalTokens),
	}

	if costErr == nil {
		logFields = append(logFields, zap.Float64("estimatedCostUSD", cost))
	} else {
		logFields = append(logFields, zap.String("costCalculationError", costErr.Error()))
	}

	oai.logger.Info("Received response from OpenAI", logFields...)

	return &aiResponse, nil
}
