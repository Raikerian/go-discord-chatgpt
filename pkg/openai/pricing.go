// Package openai provides OpenAI-related infrastructure and pricing data.
package openai

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// TokenPricing represents the cost per million tokens for input, cached input, and output.
type TokenPricing struct {
	InputPerMillion       float64  `json:"input_per_million"`        // Cost per 1 million input tokens in USD
	CachedPerMillion      *float64 `json:"cached_per_million"`       // Cost per 1 million cached input tokens in USD (nil if not supported)
	OutputPerMillion      *float64 `json:"output_per_million"`       // Cost per 1 million output tokens in USD (nil if not supported)
	AudioInputPerMillion  *float64 `json:"audio_input_per_million"`  // Cost per 1 million audio input tokens in USD (nil if not supported)
	AudioOutputPerMillion *float64 `json:"audio_output_per_million"` // Cost per 1 million audio output tokens in USD (nil if not supported)
}

// ModelInfo contains detailed information about an OpenAI model.
type ModelInfo struct {
	Name        string       `json:"name"`         // Model name/identifier
	DisplayName string       `json:"display_name"` // Human-readable display name
	Pricing     TokenPricing `json:"pricing"`      // Token pricing information
	ContextSize *int         `json:"context_size"` // Maximum context window size in tokens (nil if not specified)
}

// PricingData contains all OpenAI model pricing information.
type PricingData struct {
	Models      map[string]ModelInfo `json:"models"`       // Map of model name to model info
	LastUpdated time.Time            `json:"last_updated"` // When this pricing data was last updated
	Currency    string               `json:"currency"`     // Currency for all prices (USD)
	Note        string               `json:"note"`         // Additional notes about pricing
}

// PricingService defines the interface for pricing operations.
type PricingService interface {
	// GetPricingData returns the current OpenAI pricing data.
	GetPricingData() *PricingData

	// GetModelPricing returns pricing information for a specific model.
	GetModelPricing(modelName string) (*ModelInfo, error)

	// CalculateTokenCost calculates the cost for a given number of input and output tokens.
	CalculateTokenCost(modelName string, inputTokens, outputTokens int) (float64, error)

	// CalculateCachedTokenCost calculates the cost with cached input tokens.
	CalculateCachedTokenCost(modelName string, cachedInputTokens, newInputTokens, outputTokens int) (float64, error)

	// CalculateAudioTokenCost calculates the cost for audio input/output tokens.
	CalculateAudioTokenCost(modelName string, inputAudioTokens, outputAudioTokens int) (float64, error)

	// GetAvailableModels returns a list of all available model names.
	GetAvailableModels() []string

	// GetContextSize returns the context size for a model, or 0 if not specified.
	GetContextSize(modelName string) (int, error)
}

// pricingService implements the PricingService interface.
type pricingService struct {
	modelsFilePath string
	cachedData     *PricingData
}

// NewPricingService creates a new PricingService instance.
// modelsFilePath should be the path to the models.json file.
func NewPricingService(modelsFilePath string) PricingService {
	return &pricingService{
		modelsFilePath: modelsFilePath,
	}
}

// loadPricingData loads pricing data from the JSON file.
func (p *pricingService) loadPricingData() (*PricingData, error) {
	jsonData, err := os.ReadFile(p.modelsFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read models.json file: %w", err)
	}

	var pricingData PricingData
	if err := json.Unmarshal(jsonData, &pricingData); err != nil {
		return nil, fmt.Errorf("failed to parse models.json: %w", err)
	}

	return &pricingData, nil
}

// GetPricingData returns the current OpenAI pricing data.
func (p *pricingService) GetPricingData() *PricingData {
	if p.cachedData != nil {
		return p.cachedData
	}

	data, err := p.loadPricingData()
	if err != nil {
		// Return empty data with error information if loading fails
		return &PricingData{
			Models:      make(map[string]ModelInfo),
			LastUpdated: time.Now(),
			Currency:    "USD",
			Note:        fmt.Sprintf("Error loading pricing data: %v", err),
		}
	}

	p.cachedData = data

	return p.cachedData
}

// GetModelPricing returns pricing information for a specific model.
func (p *pricingService) GetModelPricing(modelName string) (*ModelInfo, error) {
	pricingData := p.GetPricingData()

	if model, exists := pricingData.Models[modelName]; exists {
		return &model, nil
	}

	return nil, fmt.Errorf("pricing data not found for model: %s", modelName)
}

// CalculateTokenCost calculates the cost for a given number of input and output tokens.
func (p *pricingService) CalculateTokenCost(modelName string, inputTokens, outputTokens int) (float64, error) {
	model, err := p.GetModelPricing(modelName)
	if err != nil {
		return 0, err
	}

	var totalCost float64

	// Calculate input cost
	inputCost := (float64(inputTokens) / 1_000_000) * model.Pricing.InputPerMillion
	totalCost += inputCost

	// Calculate output cost (if supported)
	if model.Pricing.OutputPerMillion != nil {
		outputCost := (float64(outputTokens) / 1_000_000) * *model.Pricing.OutputPerMillion
		totalCost += outputCost
	}

	return totalCost, nil
}

// CalculateCachedTokenCost calculates the cost with cached input tokens.
func (p *pricingService) CalculateCachedTokenCost(modelName string, cachedInputTokens, newInputTokens, outputTokens int) (float64, error) {
	model, err := p.GetModelPricing(modelName)
	if err != nil {
		return 0, err
	}

	var totalCost float64

	// Calculate cached input cost (if supported)
	if model.Pricing.CachedPerMillion != nil {
		cachedInputCost := (float64(cachedInputTokens) / 1_000_000) * *model.Pricing.CachedPerMillion
		totalCost += cachedInputCost
	} else {
		// If cached pricing not supported, use regular input pricing
		cachedInputCost := (float64(cachedInputTokens) / 1_000_000) * model.Pricing.InputPerMillion
		totalCost += cachedInputCost
	}

	// Calculate new input cost
	newInputCost := (float64(newInputTokens) / 1_000_000) * model.Pricing.InputPerMillion
	totalCost += newInputCost

	// Calculate output cost (if supported)
	if model.Pricing.OutputPerMillion != nil {
		outputCost := (float64(outputTokens) / 1_000_000) * *model.Pricing.OutputPerMillion
		totalCost += outputCost
	}

	return totalCost, nil
}

// CalculateAudioTokenCost calculates the cost for audio input/output tokens.
func (p *pricingService) CalculateAudioTokenCost(modelName string, inputAudioTokens, outputAudioTokens int) (float64, error) {
	model, err := p.GetModelPricing(modelName)
	if err != nil {
		return 0, err
	}

	var totalCost float64

	// Calculate audio input cost (if supported)
	if model.Pricing.AudioInputPerMillion != nil {
		audioInputCost := (float64(inputAudioTokens) / 1_000_000) * *model.Pricing.AudioInputPerMillion
		totalCost += audioInputCost
	}

	// Calculate audio output cost (if supported)
	if model.Pricing.AudioOutputPerMillion != nil {
		audioOutputCost := (float64(outputAudioTokens) / 1_000_000) * *model.Pricing.AudioOutputPerMillion
		totalCost += audioOutputCost
	}

	return totalCost, nil
}

// GetAvailableModels returns a list of all available model names.
func (p *pricingService) GetAvailableModels() []string {
	pricingData := p.GetPricingData()
	models := make([]string, 0, len(pricingData.Models))

	for modelName := range pricingData.Models {
		models = append(models, modelName)
	}

	return models
}

// GetContextSize returns the context size for a model, or 0 if not specified.
func (p *pricingService) GetContextSize(modelName string) (int, error) {
	model, err := p.GetModelPricing(modelName)
	if err != nil {
		return 0, err
	}

	if model.ContextSize == nil {
		return 0, nil
	}

	return *model.ContextSize, nil
}
