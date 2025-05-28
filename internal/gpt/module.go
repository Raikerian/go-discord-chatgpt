// Package gpt provides GPT-related caching infrastructure and Fx modules.
package gpt

import (
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
)

// Module provides GPT-related dependencies.
var Module = fx.Module("gpt",
	fx.Provide(
		NewMessagesCacheProvider,
		NewNegativeThreadCacheProvider,
	),
)

// NewMessagesCacheProvider creates a MessagesCache with config-derived size.
func NewMessagesCacheProvider(cfg *config.Config, logger *zap.Logger) MessagesCache {
	size := cfg.OpenAI.MessageCacheSize
	if size <= 0 {
		logger.Warn("OpenAI MessageCacheSize is not configured or is invalid, defaulting to 100",
			zap.Int("configuredSize", size))
		size = 100
	}
	logger.Info("Creating MessagesCache", zap.Int("size", size))

	return NewMessagesCache(size)
}

// NewNegativeThreadCacheProvider creates a NegativeThreadCache with config-derived size.
func NewNegativeThreadCacheProvider(cfg *config.Config, logger *zap.Logger) NegativeThreadCache {
	size := cfg.OpenAI.NegativeThreadCacheSize
	if size <= 0 {
		logger.Warn("OpenAI NegativeThreadCacheSize is not configured or is invalid, defaulting to 1000",
			zap.Int("configuredSize", size))
		size = 1000
	}
	logger.Info("Creating NegativeThreadCache", zap.Int("size", size))

	return NewNegativeThreadCache(size)
}
