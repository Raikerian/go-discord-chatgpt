// Package chat provides chat service infrastructure and Fx modules.
package chat

import (
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/diamondburned/arikawa/v3/session"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	pkgopenai "github.com/Raikerian/go-discord-chatgpt/pkg/openai"
)

// Module provides chat service dependencies.
var Module = fx.Module("chat",
	fx.Provide(
		NewDiscordInteractionManager,
		NewOpenAIProvider,
		NewConversationStoreProvider,
		NewModelSelector,
		NewSummaryParser,
		NewOpenAITitleGenerator,
		NewUsageFormatterProvider,
		NewMessageEmbedServiceProvider,
		NewService,
	),
)

// NewConversationStoreProvider creates a ConversationStore with config-derived cache sizes.
func NewConversationStoreProvider(
	logger *zap.Logger,
	cfg *config.Config,
	summaryParser SummaryParser,
) ConversationStore {
	messageCacheSize := cfg.OpenAI.MessageCacheSize
	if messageCacheSize <= 0 {
		logger.Warn("OpenAI MessageCacheSize is not configured or is invalid, defaulting to 100",
			zap.Int("configuredSize", messageCacheSize))
		messageCacheSize = 100
	}

	negativeThreadCacheSize := cfg.OpenAI.NegativeThreadCacheSize
	if negativeThreadCacheSize <= 0 {
		logger.Warn("OpenAI NegativeThreadCacheSize is not configured or is invalid, defaulting to 1000",
			zap.Int("configuredSize", negativeThreadCacheSize))
		negativeThreadCacheSize = 1000
	}

	return NewConversationStore(logger, messageCacheSize, negativeThreadCacheSize, summaryParser)
}

// NewUsageFormatterProvider creates a UsageFormatter with the pricing service.
func NewUsageFormatterProvider(pricingService pkgopenai.PricingService) UsageFormatter {
	return NewOpenAIUsageFormatter(pricingService)
}

// NewMessageEmbedServiceProvider creates a MessageEmbedService with required dependencies.
func NewMessageEmbedServiceProvider(
	ses *session.Session,
	usageFormatter UsageFormatter,
	logger *zap.Logger,
) MessageEmbedService {
	return NewDiscordEmbedService(ses, usageFormatter, logger)
}
