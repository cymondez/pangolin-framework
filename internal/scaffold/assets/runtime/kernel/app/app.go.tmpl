// Package app wires the default kernel runtime for root-composed
// applications. Run starts the composition, waits for context cancellation
// and stops the host; no services, environment or low-level arrays are
// required, and opt-in Sources override the built-in configuration defaults.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
)

// Fixed kernel runtime budgets. They are NOT configurable through
// configuration sources; the StartupRules overrides below only change the
// values modules read from kernel/config. Drain/cleanup budgets stay
// bounded constants on purpose until an explicit runtime option exists.
const (
	defaultDrainTimeout   = 10 * time.Second
	defaultCleanupTimeout = 5 * time.Second
	defaultStopBudget     = 25 * time.Second
)

// builtinDefaults holds every StartupRules required key, so a formal
// application starts with no environment or external services.
var builtinDefaults = map[string]string{
	"uow.default_timeout":        "5s",
	"uow.cleanup_timeout":        "2s",
	"uow.hook_timeout":           "1s",
	"uow.max_requires_new_depth": "4",
}

type runOptions struct {
	roots         []any
	sources       []config.Source
	startupChecks []func(context.Context, module.Runtime) error
	participants  []runtime.Participant
}

type RunOption func(*runOptions)

// Roots supplies configured module instances for dependencies in the Root
// graph. The loader still validates all declared Type edges.
func Roots(instances ...any) RunOption {
	return func(o *runOptions) { o.roots = append(o.roots, instances...) }
}

// Sources adds configuration sources. Every caller "defaults" source is
// merged over the built-in defaults inside the same layer (later sources win,
// callers override built-ins); file/environment/cli keep their usual higher
// precedence. Values are copied; the caller's maps are never aliased or
// mutated, and invalid values fail the config StartupRules validation.
func Sources(sources ...config.Source) RunOption {
	return func(o *runOptions) { o.sources = append(o.sources, sources...) }
}

// StartupChecks registers the existing post-seal, pre-Start compatibility checks.
func StartupChecks(checks ...func(context.Context, module.Runtime) error) RunOption {
	return func(o *runOptions) { o.startupChecks = append(o.startupChecks, checks...) }
}

// Participants registers optional drain participants for transports and workers.
func Participants(participants ...runtime.Participant) RunOption {
	return func(o *runOptions) { o.participants = append(o.participants, participants...) }
}

// mergedDefaults copies the built-in defaults and overlays every caller
// "defaults" source in order, producing one fresh map per Run.
func mergedDefaults(caller []config.Source) (map[string]string, []config.Source) {
	merged := make(map[string]string, len(builtinDefaults)+4)
	for k, v := range builtinDefaults {
		merged[k] = v
	}
	rest := make([]config.Source, 0, len(caller))
	for _, s := range caller {
		if s.Name == "defaults" {
			for k, v := range s.Values {
				merged[k] = v
			}
			continue
		}
		rest = append(rest, s)
	}
	return merged, rest
}

// Run starts the root composition in a default bounded runtime, waits for ctx
// cancellation and stops the host. The required kernel StartupRules keys are
// supplied as defaults, so a formal application runs with no environment or
// external services; opt-in Sources merge over them inside the same layer.
// Runtime drain/cleanup budgets are fixed constants (see above), not config.
func Run(ctx context.Context, root any, opts ...RunOption) error {
	if ctx == nil {
		return errors.New("context required")
	}
	o := runOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	defaults, sources := mergedDefaults(o.sources)
	sources = append(sources, config.Source{Name: "defaults", Values: defaults})
	host, err := runtime.New(runtime.Options{
		DrainTimeout:   defaultDrainTimeout,
		CleanupTimeout: defaultCleanupTimeout,
		StartupChecks:  o.startupChecks,
	}, sources, o.participants...)
	if err != nil {
		return err
	}
	if err = runtime.StartRoot(ctx, host, append([]any{root}, o.roots...)...); err != nil {
		return err
	}
	<-ctx.Done()
	stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultStopBudget)
	defer cancel()
	return host.Stop(stop)
}
