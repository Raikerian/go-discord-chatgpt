package chat_test

import (
	"testing"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"

	"github.com/Raikerian/go-discord-chatgpt/internal/chat"
	"github.com/Raikerian/go-discord-chatgpt/pkg/test"
)

func TestOpenAIUsageFormatter_FormatUsage_StandardCase(t *testing.T) {
	mockPricing := test.NewMockPricingService(t)

	formatter := chat.NewOpenAIUsageFormatter(mockPricing)

	usage := openai.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	mockPricing.On("CalculateTokenCost", "gpt-4", 100, 50).Return(0.0045, nil)

	result, err := formatter.FormatUsage(usage, "gpt-4")

	assert.NoError(t, err)
	assert.Contains(t, result, "Input: 100")
	assert.Contains(t, result, "Output: 50")
	assert.Contains(t, result, "Total: 150")
	assert.Contains(t, result, "Cost: $0.0045")
}

func TestOpenAIUsageFormatter_FormatUsage_WithCachedTokens(t *testing.T) {
	mockPricing := test.NewMockPricingService(t)

	formatter := chat.NewOpenAIUsageFormatter(mockPricing)

	usage := openai.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		PromptTokensDetails: &openai.PromptTokensDetails{
			CachedTokens: 30,
		},
	}

	mockPricing.On("CalculateCachedTokenCost", "gpt-4", 30, 70, 50).Return(0.0035, nil)

	result, err := formatter.FormatUsage(usage, "gpt-4")

	assert.NoError(t, err)
	assert.Contains(t, result, "Cached: 30")
	assert.Contains(t, result, "New: 70")
	assert.Contains(t, result, "Output: 50")
	assert.Contains(t, result, "Total: 150")
	assert.Contains(t, result, "Cost: $0.0035")
}

func TestOpenAIUsageFormatter_FormatUsage_CostCalculationError(t *testing.T) {
	mockPricing := test.NewMockPricingService(t)

	formatter := chat.NewOpenAIUsageFormatter(mockPricing)

	usage := openai.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	mockPricing.On("CalculateTokenCost", "unknown-model", 100, 50).Return(0.0, assert.AnError)

	result, err := formatter.FormatUsage(usage, "unknown-model")

	assert.NoError(t, err) // Should not return error, just fallback
	assert.Contains(t, result, "Input: 100")
	assert.Contains(t, result, "Output: 50")
	assert.Contains(t, result, "Total: 150")
	assert.NotContains(t, result, "Cost:") // Should not include cost when calculation fails
}
