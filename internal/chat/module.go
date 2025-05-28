// Package chat provides chat service infrastructure and Fx modules.
package chat

import (
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
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
