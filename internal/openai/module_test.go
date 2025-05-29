package openai

import (
	"testing"

	"github.com/sashabaranov/go-openai"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	pkgopenai "github.com/Raikerian/go-discord-chatgpt/pkg/openai"
)

func TestModule(t *testing.T) {
	// Create a test configuration
	testConfig := &config.Config{
		OpenAI: config.OpenAIConfig{
			APIKey: "test-api-key",
		},
	}

	// Create a test logger
	logger := zap.NewNop()

	// Test that the module provides both services correctly
	app := fxtest.New(t,
		fx.Supply(testConfig, logger),
		Module,
		fx.Invoke(func(client *openai.Client, pricingService pkgopenai.PricingService) {
			// Verify that both services are provided
			if client == nil {
				t.Error("OpenAI client should not be nil")
			}
			if pricingService == nil {
				t.Error("Pricing service should not be nil")
			}
		}),
	)

	app.RequireStart()
	app.RequireStop()
}

func TestNewPricingService(t *testing.T) {
	logger := zap.NewNop()

	service := NewPricingService(logger)
	if service == nil {
		t.Error("NewPricingService should not return nil")
	}

	// Test that the service can be used (even if models.json doesn't exist, it should handle gracefully)
	data := service.GetPricingData()
	if data == nil {
		t.Error("GetPricingData should not return nil")
	}
}
