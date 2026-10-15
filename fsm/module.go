package fsm

import (
	"github.com/go-core-fx/logger"
	"go.uber.org/fx"
)

// Module wires the FSM store into the fx graph: NewStore over the Storage
// the application provides, with a module-scoped logger.
func Module() fx.Option {
	return fx.Module(
		"fsm",
		logger.WithNamedLogger("fsm"),
		fx.Provide(
			NewStore,
		),
	)
}
