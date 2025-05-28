// Package config provides configuration infrastructure and Fx modules.
package config

import (
	"go.uber.org/fx"
)

// Module provides configuration dependencies.
var Module = fx.Module("config",
	fx.Provide(LoadConfig),
)
