// Package chat provides modular chat service components for handling Discord chat interactions with AI.
package chat

import (
	"fmt"

	"github.com/sashabaranov/go-openai"

	pkgopenai "github.com/Raikerian/go-discord-chatgpt/pkg/openai"
)

// UsageFormatter defines the interface for formatting OpenAI usage information.
type UsageFormatter interface {
	FormatUsage(usage openai.Usage, modelName string) (string, error)
}

// openAIUsageFormatter implements the UsageFormatter interface for OpenAI responses.
type openAIUsageFormatter struct {
	pricingService pkgopenai.PricingService
}

// NewOpenAIUsageFormatter creates a new OpenAI usage formatter.
func NewOpenAIUsageFormatter(pricingService pkgopenai.PricingService) UsageFormatter {
	return &openAIUsageFormatter{
		pricingService: pricingService,
	}
}

// FormatUsage formats OpenAI usage information with cost calculation when available.
func (f *openAIUsageFormatter) FormatUsage(usage openai.Usage, modelName string) (string, error) {
	cachedTokens := f.extractCachedTokens(usage)

	if cachedTokens > 0 {
		// Format with cached tokens breakdown
		cost, err := f.pricingService.CalculateCachedTokenCost(
			modelName,
			cachedTokens,
			usage.PromptTokens-cachedTokens,
			usage.CompletionTokens,
		)
		if err != nil {
			// Fallback without cost if calculation fails
			return fmt.Sprintf("Cached: %d | New: %d | Output: %d | Total: %d tokens",
				cachedTokens,
				usage.PromptTokens-cachedTokens,
				usage.CompletionTokens,
				usage.TotalTokens), nil
		}

		return fmt.Sprintf("Cached: %d | New: %d | Output: %d | Total: %d tokens\nCost: $%.6f",
			cachedTokens,
			usage.PromptTokens-cachedTokens,
			usage.CompletionTokens,
			usage.TotalTokens,
			cost), nil
	}

	// Format without cached tokens (standard case)
	cost, err := f.pricingService.CalculateTokenCost(
		modelName,
		usage.PromptTokens,
		usage.CompletionTokens,
	)
	if err != nil {
		// Fallback without cost if calculation fails
		return fmt.Sprintf("Input: %d | Output: %d | Total: %d tokens",
			usage.PromptTokens,
			usage.CompletionTokens,
			usage.TotalTokens), nil
	}

	return fmt.Sprintf("Input: %d | Output: %d | Total: %d tokens\nCost: $%.6f",
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		cost), nil
}

// extractCachedTokens extracts cached token information from usage if available.
func (f *openAIUsageFormatter) extractCachedTokens(usage openai.Usage) int {
	// Extract cached tokens from PromptTokensDetails if available
	if usage.PromptTokensDetails != nil {
		return usage.PromptTokensDetails.CachedTokens
	}

	return 0
}
