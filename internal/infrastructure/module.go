// Package infrastructure provides core infrastructure components and their Fx modules.
package infrastructure

import (
	"context"
	"fmt"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	pkginfra "github.com/Raikerian/go-discord-chatgpt/pkg/infrastructure"
)

// LoggerModule provides logging infrastructure.
var LoggerModule = fx.Module("logger",
	fx.Provide(NewZapLogger),
)

// NewZapLoggerParams holds dependencies for NewZapLogger.
type NewZapLoggerParams struct {
	fx.In
	Cfg *config.Config
	LC  fx.Lifecycle
}

// NewZapLogger creates and configures a new Zap logger.
func NewZapLogger(params NewZapLoggerParams) (*zap.Logger, error) {
	var zapConfig zap.Config
	switch params.Cfg.LogLevel {
	case "debug":
		zapConfig = zap.NewDevelopmentConfig()
	case "info":
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	logger, err := zapConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create zap logger: %w", err)
	}

	params.LC.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return logger.Sync()
		},
	})

	return logger, nil
}

// NewFxLoggerAdapter creates a new Fx logger adapter using the public package.
func NewFxLoggerAdapter(logger *zap.Logger) fxevent.Logger {
	return pkginfra.NewFxLoggerAdapter(logger)
}

// NewFxPrinter creates a new Fx printer adapter using the public package.
func NewFxPrinter(logger *zap.Logger) fx.Printer {
	return pkginfra.NewFxPrinter(logger)
}
