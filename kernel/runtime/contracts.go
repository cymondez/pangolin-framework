// Package runtime defines the host port. Bootstrap/DI/drain implementations follow in M1.
package runtime

import (
	"context"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/module"
)

type Options struct {
	DrainTimeout   time.Duration
	CleanupTimeout time.Duration
	// StartupChecks run after providers are sealed and before any Start. Persistence
	// assemblies register schema compatibility and complete Scope inventory here.
	StartupChecks []func(context.Context, module.Runtime) error
}

type Host interface {
	Start(context.Context, []module.Module) error
	Ready() bool
	Stop(context.Context) error
}
