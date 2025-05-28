// Package infrastructure provides core infrastructure components and their Fx modules.
package infrastructure

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
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

// FxLoggerAdapter adapts a zap.SugaredLogger to fx.Printer interface.
type FxLoggerAdapter struct {
	logger *zap.SugaredLogger
}

// NewFxLoggerAdapter creates a new Fx logger adapter.
func NewFxLoggerAdapter(logger *zap.Logger) fxevent.Logger {
	return &FxLoggerAdapter{logger: logger.Sugar()}
}

// NewFxPrinter creates a new Fx printer adapter.
func NewFxPrinter(logger *zap.Logger) fx.Printer {
	return &FxLoggerAdapter{logger: logger.Sugar()}
}

// LogEvent implements fxevent.Logger.
func (p *FxLoggerAdapter) LogEvent(event fxevent.Event) {
	switch e := event.(type) {
	case *fxevent.OnStartExecuting:
		p.logger.Debugf("HOOK OnStart executing: %s, function: %s", e.CallerName, e.FunctionName)
	case *fxevent.OnStartExecuted:
		p.logWithOptionalError("HOOK OnStart", e.CallerName, e.FunctionName, e.Err, e.Runtime.String())
	case *fxevent.OnStopExecuting:
		p.logger.Debugf("HOOK OnStop executing: %s, function: %s", e.CallerName, e.FunctionName)
	case *fxevent.OnStopExecuted:
		p.logWithOptionalError("HOOK OnStop", e.CallerName, e.FunctionName, e.Err, e.Runtime.String())
	case *fxevent.Supplied:
		p.logSuppliedOrProvided("SUPPLY", e.TypeName, "", e.Err)
	case *fxevent.Provided:
		p.logSuppliedOrProvided("PROVIDE", "", strings.Join(e.OutputTypeNames, ", "), e.Err)
	case *fxevent.Invoking:
		p.logger.Debugf("INVOKE: %s", e.FunctionName)
	case *fxevent.Invoked:
		p.logInvokeResult(e.FunctionName, e.Err)
	case *fxevent.Stopping:
		p.logger.Infof("STOPPING: %s", e.Signal)
	case *fxevent.Stopped:
		p.logSimpleWithError("STOPPED", e.Err)
	case *fxevent.RollingBack:
		p.logger.Errorf("ROLLING BACK: %v", e.StartErr)
	case *fxevent.RolledBack:
		p.logSimpleWithError("ROLLED BACK", e.Err)
	case *fxevent.Started:
		p.logSimpleWithError("STARTED", e.Err)
	case *fxevent.LoggerInitialized:
		p.logLoggerInitialized(e.ConstructorName, e.Err)
	default:
		p.logger.Debugf("UNKNOWN Fx event: %T", event)
	}
}

func (p *FxLoggerAdapter) logWithOptionalError(action, caller, function string, err error, runtime string) {
	if err != nil {
		p.logger.Errorf("%s failed: %s, function: %s, error: %v", action, caller, function, err)
	} else {
		p.logger.Debugf("%s executed: %s, function: %s, runtime: %s", action, caller, function, runtime)
	}
}

func (p *FxLoggerAdapter) logSuppliedOrProvided(action, typeName, outputTypes string, err error) {
	switch {
	case err != nil:
		p.logger.Errorf("%s failed: type: %s, error: %v", action, typeName, err)
	case typeName != "":
		p.logger.Debugf("%s: %s", action, typeName)
	default:
		p.logger.Debugf("%s: %s", action, outputTypes)
	}
}

func (p *FxLoggerAdapter) logInvokeResult(functionName string, err error) {
	if err != nil {
		p.logger.Errorf("INVOKE failed: %s, error: %v", functionName, err)
	} else {
		p.logger.Debugf("INVOKE successful: %s", functionName)
	}
}

func (p *FxLoggerAdapter) logSimpleWithError(action string, err error) {
	if err != nil {
		p.logger.Errorf("%s with error: %v", action, err)
	} else {
		p.logger.Info(action)
	}
}

func (p *FxLoggerAdapter) logLoggerInitialized(constructorName string, err error) {
	if err != nil {
		p.logger.Errorf("LOGGER INITIALIZED with error: %v", err)
	} else {
		p.logger.Debugf("LOGGER INITIALIZED: %s", constructorName)
	}
}

// Printf implements fx.Printer.
func (p *FxLoggerAdapter) Printf(format string, args ...interface{}) {
	p.logger.Infof(format, args...)
}
