package infrastructure_test

import (
	"errors"
	"testing"

	"github.com/Raikerian/go-discord-chatgpt/pkg/infrastructure"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewFxLoggerAdapter(t *testing.T) {
	logger := zaptest.NewLogger(t)

	adapter := infrastructure.NewFxLoggerAdapter(logger)

	// Verify it implements the correct interface
	var _ fxevent.Logger = adapter

	if adapter == nil {
		t.Fatal("NewFxLoggerAdapter returned nil")
	}
}

func TestNewFxPrinter(t *testing.T) {
	logger := zaptest.NewLogger(t)

	printer := infrastructure.NewFxPrinter(logger)

	// Verify it implements the correct interface
	var _ fx.Printer = printer

	if printer == nil {
		t.Fatal("NewFxPrinter returned nil")
	}
}

func TestFxLoggerAdapter_LogEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	adapter := infrastructure.NewFxLoggerAdapter(logger)

	// Test various event types to ensure no panics
	events := []fxevent.Event{
		&fxevent.OnStartExecuting{
			FunctionName: "testFunc",
			CallerName:   "testCaller",
		},
		&fxevent.OnStartExecuted{
			FunctionName: "testFunc",
			CallerName:   "testCaller",
			Err:          nil,
		},
		&fxevent.Provided{
			OutputTypeNames: []string{"*zap.Logger"},
		},
		&fxevent.Invoking{
			FunctionName: "testFunc",
		},
		&fxevent.Started{
			Err: nil,
		},
	}

	// Should not panic
	for _, event := range events {
		adapter.LogEvent(event)
	}
}

func TestFxLoggerAdapter_Printf(t *testing.T) {
	logger := zaptest.NewLogger(t)
	printer := infrastructure.NewFxPrinter(logger)

	// Should not panic
	printer.Printf("Test message: %s", "hello")
	printer.Printf("Test message without args")
}

func TestFxLoggerAdapter_WithErrors(t *testing.T) {
	logger := zaptest.NewLogger(t)
	adapter := infrastructure.NewFxLoggerAdapter(logger)

	testError := errors.New("test error")

	// Test events with errors
	errorEvents := []fxevent.Event{
		&fxevent.OnStartExecuted{
			FunctionName: "testFunc",
			CallerName:   "testCaller",
			Err:          testError,
		},
		&fxevent.Started{
			Err: testError,
		},
		&fxevent.LoggerInitialized{
			ConstructorName: "testConstructor",
			Err:             testError,
		},
	}

	// Should not panic even with errors
	for _, event := range errorEvents {
		adapter.LogEvent(event)
	}
}

func TestFxIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test that the adapter works with actual Fx
	app := fx.New(
		fx.WithLogger(infrastructure.NewFxLoggerAdapter),
		fx.Provide(func() *zap.Logger { return logger }),
		fx.Invoke(func(*zap.Logger) {
			// Simple function to invoke
		}),
	)

	// Should not panic during creation
	if app == nil {
		t.Fatal("Failed to create Fx app with logger adapter")
	}
}
