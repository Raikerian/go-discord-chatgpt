package openai_test

import (
	"fmt"
	"log"

	"github.com/Raikerian/go-discord-chatgpt/pkg/openai"
)

func ExamplePricingService_GetPricingData() {
	// Create a pricing service (in real usage, you'd pass the actual path to models.json)
	service := openai.NewPricingService("models.json")
	data := service.GetPricingData()

	fmt.Printf("Currency: %s\n", data.Currency)
	fmt.Printf("Number of models: %d\n", len(data.Models))
	// Note: This example may not produce exact output due to file loading,
	// but demonstrates the API usage
}

func ExamplePricingService_GetModelPricing() {
	service := openai.NewPricingService("models.json")
	model, err := service.GetModelPricing("gpt-4")
	if err != nil {
		log.Printf("Error getting model pricing: %v", err)
		return
	}

	fmt.Printf("Model: %s\n", model.DisplayName)
	fmt.Printf("Input cost per million: $%.2f\n", model.Pricing.InputPerMillion)
	if model.Pricing.CachedPerMillion != nil {
		fmt.Printf("Cached input cost per million: $%.2f\n", *model.Pricing.CachedPerMillion)
	}
	if model.Pricing.OutputPerMillion != nil {
		fmt.Printf("Output cost per million: $%.2f\n", *model.Pricing.OutputPerMillion)
	}
	if model.ContextSize != nil {
		fmt.Printf("Context size: %d tokens\n", *model.ContextSize)
	} else {
		fmt.Printf("Context size: variable/unspecified\n")
	}
}

func ExamplePricingService_CalculateTokenCost() {
	service := openai.NewPricingService("models.json")

	// Calculate cost for 1000 input tokens and 500 output tokens
	cost, err := service.CalculateTokenCost("gpt-4", 1000, 500)
	if err != nil {
		log.Printf("Error calculating token cost: %v", err)
		return
	}

	fmt.Printf("Cost for 1000 input + 500 output tokens: $%.6f\n", cost)
}

func ExamplePricingService_CalculateCachedTokenCost() {
	service := openai.NewPricingService("models.json")

	// Calculate cost with 500 cached input tokens, 500 new input tokens, and 300 output tokens
	cost, err := service.CalculateCachedTokenCost("gpt-4", 500, 500, 300)
	if err != nil {
		log.Printf("Error calculating cached token cost: %v", err)
		return
	}

	fmt.Printf("Cost with cached tokens: $%.6f\n", cost)
}

func ExamplePricingService_GetContextSize() {
	service := openai.NewPricingService("models.json")

	// Get context size for a model with defined context size
	contextSize, err := service.GetContextSize("gpt-4")
	if err != nil {
		log.Printf("Error getting context size: %v", err)
		return
	}
	fmt.Printf("gpt-4 context size: %d tokens\n", contextSize)

	// Get context size for a model without defined context size
	contextSize, err = service.GetContextSize("gpt-3.5-turbo")
	if err != nil {
		log.Printf("Error getting context size: %v", err)
		return
	}
	fmt.Printf("gpt-3.5-turbo context size: %d tokens (0 means unspecified)\n", contextSize)
}

// ExampleNewPricingService demonstrates how to create a pricing service
func ExampleNewPricingService() {
	// Create a pricing service with the path to your models.json file
	service := openai.NewPricingService("./models.json")

	// Get available models
	models := service.GetAvailableModels()
	fmt.Printf("Available models: %d\n", len(models))
}
