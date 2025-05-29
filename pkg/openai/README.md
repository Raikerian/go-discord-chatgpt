# OpenAI Pricing Package

This package provides comprehensive OpenAI model pricing information and cost calculation utilities.

## Features

- **Service-Based Architecture**: Uses a `PricingService` interface for clean dependency injection and testing
- **Dynamic Pricing Data**: Loads pricing information from a JSON file at runtime instead of hardcoded values
- **Cached Input Support**: Handles models that support cached input tokens with different pricing
- **Output Token Support**: Handles models that may not support output tokens (e.g., image generators)
- **Thread-Safe**: Uses mutex for safe concurrent access to pricing data
- **Comprehensive Model Coverage**: Includes all major OpenAI models including GPT-4.1, GPT-4.5, o1, o3, and more

## Usage

### Creating a Pricing Service

```go
// Create a new pricing service instance
service := openai.NewPricingService("models.json")
```

### Basic Pricing Information

```go
// Get all pricing data
data := service.GetPricingData()
fmt.Printf("Currency: %s\n", data.Currency)
fmt.Printf("Number of models: %d\n", len(data.Models))

// Get specific model pricing
model, err := service.GetModelPricing("gpt-4o")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Input cost: $%.2f per million tokens\n", model.Pricing.InputPerMillion)
```

### Cost Calculations

```go
// Calculate cost for regular tokens
cost, err := service.CalculateTokenCost("gpt-4o", 1000, 500) // 1000 input, 500 output
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Total cost: $%.6f\n", cost)

// Calculate cost with cached input tokens
cost, err := service.CalculateCachedTokenCost("gpt-4o", 500, 500, 300) // 500 cached, 500 new input, 300 output
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Total cost with caching: $%.6f\n", cost)
```

### Available Models

```go
// Get all available models
models := service.GetAvailableModels()
fmt.Printf("Available models: %v\n", models)
```

### Context Size Information

```go
// Get context size for a model (returns 0 if not specified)
contextSize, err := service.GetContextSize("gpt-4o")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Context size: %d tokens\n", contextSize)

// Check if model has a defined context size by comparing to 0
if contextSize > 0 {
    fmt.Println("Model has a defined context size")
} else {
    fmt.Println("Model has variable or unspecified context size")
}
```

## Data Structure

The pricing data is loaded from `models.json` and includes:

- **Input Pricing**: Cost per million input tokens
- **Cached Input Pricing**: Cost per million cached input tokens (if supported)
- **Output Pricing**: Cost per million output tokens (if supported)
- **Context Size**: Maximum context window size (if specified, null for variable/unspecified)
- **Display Names**: Human-readable model names

## Supported Models

The package includes pricing for 40+ OpenAI models including:

- GPT-4.1 series (gpt-4.1, gpt-4.1-mini, gpt-4.1-nano)
- GPT-4.5 preview models
- GPT-4o series (including audio and realtime variants)
- o1, o3, and o4 series
- Specialized models (computer-use, search, image generation)

## Configuration

The pricing data is automatically loaded from `models.json` file. The file path is specified when creating the `PricingService` instance.

## Thread Safety

All functions are thread-safe and can be called concurrently. The package uses internal caching to ensure safe access to pricing data.

## Error Handling

The package provides comprehensive error handling:

- Returns errors for non-existent models
- Gracefully handles missing JSON files (returns empty data with error message)
- Validates JSON structure during parsing

## Testing

The package includes comprehensive tests covering:

- Basic functionality tests
- Example tests with expected outputs
- Error condition testing
- Model capability testing

Run tests with:
```bash
go test ./pkg/openai -v
``` 