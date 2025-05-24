package chat

import (
	"errors"

	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
)

// ModelSelector defines the interface for selecting an AI model.
type ModelSelector interface {
	SelectModel(userPreference string) (modelName string, err error)
}

// NewConfigModelSelector creates a new ModelSelector based on application configuration.
func NewConfigModelSelector(logger *zap.Logger, cfg *config.Config) ModelSelector {
	return &configModelSelector{
		logger: logger.Named("model_selector"),
		cfg:    cfg,
	}
}

// NewModelSelector creates a new ModelSelector implementation.
func NewModelSelector(logger *zap.Logger, cfg *config.Config) ModelSelector {
	return &configModelSelector{
		logger: logger.Named("model_selector"),
		cfg:    cfg,
	}
}

type configModelSelector struct {
	logger *zap.Logger
	cfg    *config.Config
}

// SelectModel validates model configuration and selects the model to use.
func (cms *configModelSelector) SelectModel(userPreference string) (string, error) {
	if len(cms.cfg.OpenAI.Models) == 0 {
		return "", errors.New("no OpenAI models configured")
	}

	if userPreference != "" {
		for _, configuredModel := range cms.cfg.OpenAI.Models {
			if userPreference == configuredModel {
				cms.logger.Debug("Using user-specified model", zap.String("model", userPreference))

				return userPreference, nil
			}
		}
		cms.logger.Warn("User specified an invalid model, defaulting.",
			zap.String("specifiedModel", userPreference),
			zap.Strings("availableModels", cms.cfg.OpenAI.Models),
		)
	}

	defaultModel := cms.cfg.OpenAI.Models[0]
	cms.logger.Debug("Using default model", zap.String("model", defaultModel))

	return defaultModel, nil // Default to the first configured model
}
