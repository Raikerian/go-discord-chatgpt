// Package infrastructure provides reusable infrastructure components for Go applications.
package infrastructure

import (
	"strings"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

// FxLoggerAdapter adapts a zap.SugaredLogger to fx.Printer and fxevent.Logger interfaces.
// This allows using Zap logger for Fx framework's internal logging.
type FxLoggerAdapter struct {
	logger *zap.SugaredLogger
}

// NewFxLoggerAdapter creates a new Fx logger adapter that implements fxevent.Logger.
// This is useful for integrating Zap logging with Fx dependency injection framework.
func NewFxLoggerAdapter(logger *zap.Logger) fxevent.Logger {
	return &FxLoggerAdapter{logger: logger.Sugar()}
}

// NewFxPrinter creates a new Fx printer adapter that implements fx.Printer.
// This is useful for integrating Zap logging with Fx framework's print operations.
func NewFxPrinter(logger *zap.Logger) fx.Printer {
	return &FxLoggerAdapter{logger: logger.Sugar()}
}

// LogEvent implements fxevent.Logger interface.
// It handles various Fx lifecycle events and logs them appropriately using Zap.
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

// Printf implements fx.Printer interface.
// It formats and logs messages using the underlying Zap logger.
func (p *FxLoggerAdapter) Printf(format string, args ...any) {
	p.logger.Infof(format, args...)
}

// logWithOptionalError logs lifecycle events with optional error information.
func (p *FxLoggerAdapter) logWithOptionalError(action, caller, function string, err error, runtime string) {
	if err != nil {
		p.logger.Errorf("%s failed: %s, function: %s, error: %v", action, caller, function, err)
	} else {
		p.logger.Debugf("%s executed: %s, function: %s, runtime: %s", action, caller, function, runtime)
	}
}

// logSuppliedOrProvided logs supply and provide events with optional error information.
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

// logInvokeResult logs the result of function invocations.
func (p *FxLoggerAdapter) logInvokeResult(functionName string, err error) {
	if err != nil {
		p.logger.Errorf("INVOKE failed: %s, error: %v", functionName, err)
	} else {
		p.logger.Debugf("INVOKE successful: %s", functionName)
	}
}

// logSimpleWithError logs simple events with optional error information.
func (p *FxLoggerAdapter) logSimpleWithError(action string, err error) {
	if err != nil {
		p.logger.Errorf("%s with error: %v", action, err)
	} else {
		p.logger.Info(action)
	}
}

// logLoggerInitialized logs logger initialization events.
func (p *FxLoggerAdapter) logLoggerInitialized(constructorName string, err error) {
	if err != nil {
		p.logger.Errorf("LOGGER INITIALIZED with error: %v", err)
	} else {
		p.logger.Debugf("LOGGER INITIALIZED: %s", constructorName)
	}
}
