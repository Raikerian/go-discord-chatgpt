// Package openai provides OpenAI-related infrastructure and Fx modules.
package openai

import (
	"errors"

	"github.com/sashabaranov/go-openai"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	pkgopenai "github.com/Raikerian/go-discord-chatgpt/pkg/openai"
)

// Module provides OpenAI-related dependencies.
var Module = fx.Module("openai",
	fx.Provide(
		NewClient,
		NewPricingService,
	),
)

// NewClient creates and configures a new OpenAI client.
func NewClient(cfg *config.Config, logger *zap.Logger) (*openai.Client, error) {
	if cfg.OpenAI.APIKey == "" {
		logger.Error("OpenAI API key is not configured in config.yaml")

		return nil, errors.New("OpenAI API key (config.OpenAI.APIKey) is not configured")
	}

	client := openai.NewClient(cfg.OpenAI.APIKey)
	logger.Info("OpenAI client created successfully.")

	return client, nil
}

// NewPricingService creates and configures a new OpenAI pricing service.
func NewPricingService(logger *zap.Logger) pkgopenai.PricingService {
	// Use models.json from the project root
	service := pkgopenai.NewPricingService("models.json")
	logger.Info("OpenAI pricing service created successfully.")

	return service
}
