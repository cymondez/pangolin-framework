package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/di"
	"github.com/pangolin-framework/pangolin/kernel/module"
)

// RootHost is the additive root-composition entry on the same built-in host.
// The legacy Host.Start(ctx, []module.Module) path is unchanged; root mode
// activates only through StartRoot.
type RootHost interface {
	StartRoot(ctx context.Context, roots []any) error
}

// StartRoot asserts the host supports root composition and starts it with the
// typed root modules. Roots are module instances declaring
// module.ModuleMetadata/module.DependsOn; the closure, deduplication and
// validation happen in kernel/module before any hook or constructor runs.
func StartRoot(ctx context.Context, host Host, roots ...any) error {
	if host == nil {
		return fmt.Errorf("host is required")
	}
	rootHost, ok := host.(RootHost)
	if !ok {
		return fmt.Errorf("host does not support root composition")
	}
	return rootHost.StartRoot(ctx, roots)
}

// engineComposition adapts the module composition engine (kernel/module) to
// the host. The engine stamps every registry operation with the owner the
// host declares; the host drives hook windows and revokes them afterwards.
type engineComposition struct{ engine module.Composition }

func newRootComposition(plan module.RootPlan, cleanupTimeout time.Duration) (engineComposition, error) {
	engine, err := module.NewComposition(plan, cleanupTimeout)
	return engineComposition{engine}, err
}

func (e engineComposition) services(owner string) module.Services {
	if services, err := e.engine.Services(owner); err == nil {
		return services
	}
	return erroringServices{owner: owner}
}

func (e engineComposition) provide(owner string, p di.Provider) error {
	return e.engine.Provide(owner, p)
}

func (e engineComposition) replace(owner string, p di.Provider, reason string) error {
	return e.engine.Replace(owner, p, reason)
}

func (e engineComposition) revoke(owner string) { e.engine.Revoke(owner) }

func (e engineComposition) seal(ctx context.Context) (di.Container, error) {
	return e.engine.Seal(ctx)
}

func (e engineComposition) lifecycleResolver(owner string) (di.Resolver, error) {
	return e.engine.LifecycleResolver(owner)
}

// erroringServices is handed out after the owner's window was revoked: every
// operation reports the revoked window instead of nil-panicking the hook.
type erroringServices struct{ owner string }

func (s erroringServices) Add(any, ...module.AddOption) error {
	return fmt.Errorf("composition registrar for %s is no longer active", s.owner)
}
func (s erroringServices) Replace(any, string, ...module.AddOption) error {
	return fmt.Errorf("composition registrar for %s is no longer active", s.owner)
}
func (s erroringServices) Export(di.Token) error {
	return fmt.Errorf("composition registrar for %s is no longer active", s.owner)
}
func (s erroringServices) Bind(di.Token, any) error {
	return fmt.Errorf("composition registrar for %s is no longer active", s.owner)
}
func (s erroringServices) ReplaceBinding(di.Token, any, string) error {
	return fmt.Errorf("composition registrar for %s is no longer active", s.owner)
}
func (s erroringServices) ReplaceContract(di.Token, any, string, ...module.AddOption) error {
	return fmt.Errorf("composition registrar for %s is no longer active", s.owner)
}

// StartRoot composes the root closure: validate-before-hooks, explicit phase
// barriers, config, provenance-checked seal, startup checks, scope registry,
// then Start in topo order with restricted per-module resolvers. Ready only
// after every Start succeeds; failure reverses stops and closes the container.
func (h *host) StartRoot(ctx context.Context, roots []any) (err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if contextIsNil(ctx) || h.attempted || h.stopped {
		return fmt.Errorf("runtime cannot start")
	}
	h.attempted = true
	defer func() {
		if p := recover(); p != nil {
			h.stopped = true
			_ = h.shutdown(ctx)
			panic(p)
		}
		if err != nil {
			h.stopped = true
			err = errors.Join(err, h.shutdown(ctx))
		}
	}()
	plan, err := module.LoadRoot(roots...)
	if err != nil {
		return err
	}
	comp, err := newRootComposition(plan, h.options.CleanupTimeout)
	if err != nil {
		return err
	}
	defaults := map[string]string{}
	// Full phase barriers: all modules per phase, then the next phase. Each
	// hook gets a trusted builder stamped with its owner; the builder and the
	// engine-side registrar handles are revoked when the hook exits, so
	// operations outside attributed hook context are rejected.
	for _, phase := range []struct {
		name     string
		selector func(instanceHooks) func(module.RootBuilder) error
	}{
		{"preconfigure", func(hooks instanceHooks) func(module.RootBuilder) error { return hooks.preConfigure }},
		{"configure", func(hooks instanceHooks) func(module.RootBuilder) error { return hooks.configure }},
		{"postconfigure", func(hooks instanceHooks) func(module.RootBuilder) error { return hooks.postConfigure }},
	} {
		for _, m := range plan.Plan.Ordered {
			if err = ctx.Err(); err != nil {
				return err
			}
			hooks, hookErr := instanceHooksOf(plan, m.Name)
			if hookErr != nil {
				return fmt.Errorf("%s %s: %w", phase.name, m.Name, hookErr)
			}
			hook := phase.selector(hooks)
			if hook == nil {
				continue
			}
			b := newRootBuilder(comp, m.Name, defaults)
			if err = runRootHook(hook, b); err != nil {
				return fmt.Errorf("%s %s: %w", phase.name, m.Name, err)
			}
		}
	}
	if h.reader, err = h.readerWithDefaults(defaults); err != nil {
		return err
	}
	if h.container, err = comp.seal(ctx); err != nil {
		return err
	}
	if err = h.runStartupChecks(ctx); err != nil {
		return err
	}
	if err = h.checkScopeKinds(ctx, plan.Plan.Ordered); err != nil {
		return err
	}
	for _, m := range plan.Plan.Ordered {
		if err = ctx.Err(); err != nil {
			return err
		}
		hooks, hookErr := instanceHooksOf(plan, m.Name)
		if hookErr != nil {
			return fmt.Errorf("start %s: %w", m.Name, hookErr)
		}
		resolver, resolveErr := comp.lifecycleResolver(m.Name)
		if resolveErr != nil {
			return fmt.Errorf("start %s: %w", m.Name, resolveErr)
		}
		if hooks.start != nil {
			if err = hooks.start(ctx, view{resolver, h.reader}); err != nil {
				return fmt.Errorf("start %s: %w", m.Name, err)
			}
		}
		h.rootStarted = append(h.rootStarted, rootStarted{name: m.Name, services: resolver, reader: h.reader, stop: hooks.stop})
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	h.ready.Store(true)
	return nil
}

// runRootHook guarantees the builder/engine window closes on every exit,
// including a returned error or a panic: a hook cannot keep its window open
// past the phase boundary or reuse a saved builder later. Hook panics are
// rethrown unchanged (original value and type, like the legacy Start path),
// and the host failure path reclaims resources the same way; returned errors
// keep the call site's phase/module wrapping.
func runRootHook(hook func(module.RootBuilder) error, b *rootBuilder) error {
	defer b.revoke()
	return hook(b)
}

func compRevoke(c rootComposition, owner string) {
	if engine, ok := c.(engineComposition); ok {
		engine.revoke(owner)
	}
}

func instanceHooksOf(plan module.RootPlan, name string) (instanceHooks, error) {
	instance, ok := plan.Instances[name]
	if !ok {
		return instanceHooks{}, fmt.Errorf("missing instance")
	}
	if isNilHook(instance) {
		return instanceHooks{}, fmt.Errorf("nil root module instance")
	}
	hooks := instanceHooks{}
	if typed, ok := instance.(module.PreConfigurer); ok {
		hooks.preConfigure = typed.PreConfigure
	}
	if typed, ok := instance.(module.Configurer); ok {
		hooks.configure = typed.Configure
	}
	if typed, ok := instance.(module.PostConfigurer); ok {
		hooks.postConfigure = typed.PostConfigure
	}
	if typed, ok := instance.(module.Starter); ok {
		hooks.start = typed.Start
	}
	if typed, ok := instance.(module.Stopper); ok {
		hooks.stop = typed.Stop
	}
	return hooks, nil
}

// isNilHook rejects nil interfaces and typed-nil pointers as hook receivers.
func isNilHook(v any) bool {
	if v == nil {
		return true
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return value.IsNil()
	}
	return false
}

// instanceHooks is the resolved optional hook set for one root instance.
type instanceHooks struct {
	preConfigure  func(module.RootBuilder) error
	configure     func(module.RootBuilder) error
	postConfigure func(module.RootBuilder) error
	start         func(context.Context, module.Runtime) error
	stop          func(context.Context, module.Runtime) error
}

// rootComposition is the host-side seam over composition operations; the
// concrete adapter above forwards to the module engine.
type rootComposition interface {
	services(owner string) module.Services
	provide(owner string, p di.Provider) error
	replace(owner string, p di.Provider, reason string) error
}

// rootBuilder is the trusted per-module composition builder. It stamps the
// owner on every registration path and rejects everything after its hook
// returned; SetDefault follows the legacy module.Builder default-key rules.
// The engine registrar is captured once per window so a stashed handle stays
// bound to the epoch the engine revoked. A per-window mutex serializes
// operations against revoke: Revoke waits for in-flight operations to
// complete, so no SetDefault write or engine registration lands after the
// revoke barrier; the engine keeps its own session guards for Services.
type rootBuilder struct {
	comp     rootComposition
	owner    string
	defaults map[string]string
	services module.Services

	mu       sync.Mutex
	live     bool
	inFlight sync.WaitGroup
}

func newRootBuilder(comp rootComposition, owner string, defaults map[string]string) *rootBuilder {
	b := &rootBuilder{comp: comp, owner: owner, defaults: defaults, live: true}
	b.services = comp.services(owner)
	return b
}

func (b *rootBuilder) begin() error {
	b.mu.Lock()
	if !b.live {
		b.mu.Unlock()
		return fmt.Errorf("composition builder for %s is no longer active", b.owner)
	}
	b.inFlight.Add(1)
	b.mu.Unlock()
	return nil
}

func (b *rootBuilder) end() { b.inFlight.Done() }

// revoke closes the window and waits for in-flight operations: after it
// returns, no SetDefault write or engine registration from this builder can
// still land. Engine-side handles were invalidated by Revoke(owner) first.
func (b *rootBuilder) revoke() {
	b.mu.Lock()
	b.live = false
	b.mu.Unlock()
	b.inFlight.Wait()
	compRevoke(b.comp, b.owner)
}

// SetDefault records a module default. The live check, the duplicate check
// and the map mutation all happen inside one critical section, so parallel
// operations on the same builder cannot corrupt the defaults map or race the
// duplicate check; each caller sees the settled key set and no write can
// interleave with revoke. Global defaults are only written through a hook's
// builder window.
func (b *rootBuilder) SetDefault(k, v string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.live {
		return fmt.Errorf("composition builder for %s is no longer active", b.owner)
	}
	if k == "" {
		return fmt.Errorf("empty default key")
	}
	if _, ok := b.defaults[k]; ok {
		return fmt.Errorf("duplicate default %s", k)
	}
	b.defaults[k] = v
	return nil
}

// Services returns the engine registrar captured for this owner's window.
// After the hook returned, both the builder and the captured engine handle
// are revoked: the engine epoch rejects the handle, so a hook cannot stash
// the registrar and register later.
func (b *rootBuilder) Services() module.Services {
	return b.services
}

func (b *rootBuilder) Provide(p di.Provider) error {
	if err := b.begin(); err != nil {
		return err
	}
	defer b.end()
	return b.comp.provide(b.owner, p)
}

func (b *rootBuilder) Replace(p di.Provider, reason string) error {
	if err := b.begin(); err != nil {
		return err
	}
	defer b.end()
	return b.comp.replace(b.owner, p, reason)
}

// rootStarted tracks one successfully started root module for reverse cleanup.
type rootStarted struct {
	name     string
	services di.Resolver
	reader   config.Reader
	stop     func(context.Context, module.Runtime) error
}
