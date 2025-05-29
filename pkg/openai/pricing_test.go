package openai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// createTempModelsFile creates a temporary models.json file for testing.
func createTempModelsFile(t *testing.T, data *PricingData) string {
	t.Helper()

	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "models.json")

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Failed to marshal test data: %v", err)
	}

	err = os.WriteFile(tempFile, jsonData, 0644)
	if err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	return tempFile
}

// getTestPricingData returns test pricing data for use in tests.
func getTestPricingData() *PricingData {
	return &PricingData{
		Models: map[string]ModelInfo{
			"gpt-4": {
				Name:        "gpt-4",
				DisplayName: "GPT-4",
				Pricing: TokenPricing{
					InputPerMillion:  30.0,
					CachedPerMillion: &[]float64{15.0}[0],
					OutputPerMillion: &[]float64{60.0}[0],
				},
				ContextSize: &[]int{8192}[0],
			},
			"gpt-3.5-turbo": {
				Name:        "gpt-3.5-turbo",
				DisplayName: "GPT-3.5 Turbo",
				Pricing: TokenPricing{
					InputPerMillion:  1.5,
					OutputPerMillion: &[]float64{2.0}[0],
				},
				ContextSize: &[]int{4096}[0],
			},
		},
		LastUpdated: time.Now(),
		Currency:    "USD",
		Note:        "Test pricing data",
	}
}

func TestNewPricingService(t *testing.T) {
	service := NewPricingService("test-models.json")
	if service == nil {
		t.Error("NewPricingService() returned nil")
	}
}

func TestPricingService_GetPricingData(t *testing.T) {
	tests := []struct {
		name        string
		setupFile   func(t *testing.T) string
		expectError bool
	}{
		{
			name: "valid pricing data",
			setupFile: func(t *testing.T) string {
				return createTempModelsFile(t, getTestPricingData())
			},
			expectError: false,
		},
		{
			name: "file read error",
			setupFile: func(t *testing.T) string {
				return "nonexistent-file.json"
			},
			expectError: true,
		},
		{
			name: "invalid JSON",
			setupFile: func(t *testing.T) string {
				tempDir := t.TempDir()
				tempFile := filepath.Join(tempDir, "invalid.json")
				err := os.WriteFile(tempFile, []byte("invalid json"), 0644)
				if err != nil {
					t.Fatalf("Failed to write invalid JSON file: %v", err)
				}
				return tempFile
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := tt.setupFile(t)
			service := NewPricingService(filePath)

			data := service.GetPricingData()
			if data == nil {
				t.Error("GetPricingData() returned nil")
				return
			}

			if tt.expectError {
				// For error cases, check if the note contains error information
				if data.Note == "" {
					t.Error("Expected error information in Note field")
				}
			} else {
				// For success cases, verify the data
				if len(data.Models) == 0 {
					t.Error("Expected models in pricing data")
				}
			}
		})
	}
}

func TestPricingService_GetModelPricing(t *testing.T) {
	filePath := createTempModelsFile(t, getTestPricingData())
	service := NewPricingService(filePath)

	tests := []struct {
		name      string
		modelName string
		wantErr   bool
	}{
		{
			name:      "existing model",
			modelName: "gpt-4",
			wantErr:   false,
		},
		{
			name:      "non-existing model",
			modelName: "non-existent-model",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := service.GetModelPricing(tt.modelName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetModelPricing() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && model == nil {
				t.Error("GetModelPricing() returned nil model for existing model")
			}
		})
	}
}

func TestPricingService_CalculateTokenCost(t *testing.T) {
	filePath := createTempModelsFile(t, getTestPricingData())
	service := NewPricingService(filePath)

	tests := []struct {
		name         string
		modelName    string
		inputTokens  int
		outputTokens int
		wantCost     float64
		wantErr      bool
	}{
		{
			name:         "gpt-4 with input and output",
			modelName:    "gpt-4",
			inputTokens:  1000,
			outputTokens: 500,
			wantCost:     0.060, // (1000/1M * 30) + (500/1M * 60) = 0.03 + 0.03 = 0.06
			wantErr:      false,
		},
		{
			name:         "gpt-3.5-turbo input only",
			modelName:    "gpt-3.5-turbo",
			inputTokens:  1000,
			outputTokens: 0,
			wantCost:     0.0015, // 1000/1M * 1.5 = 0.0015
			wantErr:      false,
		},
		{
			name:         "non-existent model",
			modelName:    "non-existent",
			inputTokens:  1000,
			outputTokens: 500,
			wantCost:     0,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, err := service.CalculateTokenCost(tt.modelName, tt.inputTokens, tt.outputTokens)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateTokenCost() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cost != tt.wantCost {
				t.Errorf("CalculateTokenCost() = %v, want %v", cost, tt.wantCost)
			}
		})
	}
}

func TestPricingService_CalculateCachedTokenCost(t *testing.T) {
	filePath := createTempModelsFile(t, getTestPricingData())
	service := NewPricingService(filePath)

	tests := []struct {
		name              string
		modelName         string
		cachedInputTokens int
		newInputTokens    int
		outputTokens      int
		wantCost          float64
		wantErr           bool
	}{
		{
			name:              "gpt-4 with cached and new input",
			modelName:         "gpt-4",
			cachedInputTokens: 500,
			newInputTokens:    500,
			outputTokens:      500,
			wantCost:          0.0525, // (500/1M * 15) + (500/1M * 30) + (500/1M * 60) = 0.0075 + 0.015 + 0.03 = 0.0525
			wantErr:           false,
		},
		{
			name:              "gpt-3.5-turbo no cached pricing",
			modelName:         "gpt-3.5-turbo",
			cachedInputTokens: 500,
			newInputTokens:    500,
			outputTokens:      500,
			wantCost:          0.0025, // (500/1M * 1.5) + (500/1M * 1.5) + (500/1M * 2.0) = 0.00075 + 0.00075 + 0.001 = 0.0025
			wantErr:           false,
		},
		{
			name:              "non-existent model",
			modelName:         "non-existent",
			cachedInputTokens: 500,
			newInputTokens:    500,
			outputTokens:      500,
			wantCost:          0,
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, err := service.CalculateCachedTokenCost(tt.modelName, tt.cachedInputTokens, tt.newInputTokens, tt.outputTokens)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateCachedTokenCost() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cost != tt.wantCost {
				t.Errorf("CalculateCachedTokenCost() = %v, want %v", cost, tt.wantCost)
			}
		})
	}
}

func TestPricingService_GetAvailableModels(t *testing.T) {
	filePath := createTempModelsFile(t, getTestPricingData())
	service := NewPricingService(filePath)

	models := service.GetAvailableModels()
	expectedCount := 2 // gpt-4 and gpt-3.5-turbo
	if len(models) != expectedCount {
		t.Errorf("GetAvailableModels() returned %d models, want %d", len(models), expectedCount)
	}

	// Check that expected models are present
	modelSet := make(map[string]bool)
	for _, model := range models {
		modelSet[model] = true
	}

	expectedModels := []string{"gpt-4", "gpt-3.5-turbo"}
	for _, expected := range expectedModels {
		if !modelSet[expected] {
			t.Errorf("GetAvailableModels() missing expected model: %s", expected)
		}
	}
}

func TestPricingService_GetContextSize(t *testing.T) {
	filePath := createTempModelsFile(t, getTestPricingData())
	service := NewPricingService(filePath)

	tests := []struct {
		name      string
		modelName string
		wantSize  int
		wantErr   bool
	}{
		{
			name:      "model with context size",
			modelName: "gpt-4",
			wantSize:  8192,
			wantErr:   false,
		},
		{
			name:      "model without context size",
			modelName: "gpt-3.5-turbo",
			wantSize:  4096,
			wantErr:   false,
		},
		{
			name:      "non-existent model",
			modelName: "non-existent",
			wantSize:  0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, err := service.GetContextSize(tt.modelName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetContextSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if size != tt.wantSize {
				t.Errorf("GetContextSize() = %v, want %v", size, tt.wantSize)
			}
		})
	}
}
