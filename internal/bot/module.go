// Package bot provides bot service infrastructure and Fx modules.
package bot

import (
	"go.uber.org/fx"
)

// Module provides bot service dependencies.
var Module = fx.Module("bot",
	fx.Provide(NewBot),
)
