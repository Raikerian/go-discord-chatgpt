// Package commands provides command infrastructure and Fx modules.
package commands

import (
	"go.uber.org/fx"
)

// Module provides command-related dependencies.
var Module = fx.Module("commands",
	fx.Provide(
		NewCommandManager,
		// Command providers with proper grouping
		fx.Annotate(
			NewPingCommand,
			fx.As(new(Command)),
			fx.ResultTags(`group:"commands"`),
		),
		fx.Annotate(
			NewVersionCommand,
			fx.As(new(Command)),
			fx.ResultTags(`group:"commands"`),
		),
		fx.Annotate(
			NewChatCommand,
			fx.As(new(Command)),
			fx.ResultTags(`group:"commands"`),
		),
	),
)
