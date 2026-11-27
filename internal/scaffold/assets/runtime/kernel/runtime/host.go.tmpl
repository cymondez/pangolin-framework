package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/kernel/di"
	"github.com/cymondez/pangolin-framework/kernel/module"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// Participant stops admission before draining; cancellation must stop active work.
// Drain must respect its deadline, including during the final cleanup wait.
type Participant interface {
	Quiesce(context.Context) error
	Drain(context.Context) error
	Cancel()
}

type host struct {
	mu           sync.Mutex
	ready        atomic.Bool
	options      Options
	sources      []config.Source
	participants []Participant
	started      []module.Module
	rootStarted  []rootStarted
	container    di.Container
	reader       config.Reader
	attempted    bool
	stopped      bool
}

// New creates a one-shot host. A failed/stopped host is replaced, never restarted.
func New(options Options, sources []config.Source, participants ...Participant) (Host, error) {
	if options.DrainTimeout <= 0 || options.CleanupTimeout <= 0 {
		return nil, fmt.Errorf("runtime timeouts must be positive")
	}
	h := &host{options: options, participants: append([]Participant(nil), participants...)}
	h.options.StartupChecks = append([]func(context.Context, module.Runtime) error(nil), options.StartupChecks...)
	for _, check := range h.options.StartupChecks {
		if check == nil {
			return nil, fmt.Errorf("nil startup check")
		}
	}
	for _, p := range participants {
		if p == nil {
			return nil, fmt.Errorf("nil drain participant")
		}
	}
	for _, s := range sources {
		v := map[string]string{}
		for k, value := range s.Values {
			v[k] = value
		}
		h.sources = append(h.sources, config.Source{Name: s.Name, Values: v})
	}
	return h, nil
}

type builder struct {
	di.Builder
	defaults map[string]string
}

func (b *builder) SetDefault(k, v string) error {
	if k == "" {
		return fmt.Errorf("empty default key")
	}
	if _, ok := b.defaults[k]; ok {
		return fmt.Errorf("duplicate default %s", k)
	}
	b.defaults[k] = v
	return nil
}

type view struct {
	services di.Resolver
	reader   config.Reader
}

func (v view) Services() di.Resolver { return v.services }
func contextIsNil(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	value := reflect.ValueOf(ctx)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (v view) Config() config.Reader { return v.reader }
func (v view) OpenScope(ctx context.Context) (di.Scope, error) {
	if contextIsNil(ctx) {
		return nil, fmt.Errorf("runtime OpenScope requires a context")
	}
	if container, ok := v.services.(di.Container); ok {
		return container.OpenScope(ctx)
	}
	return module.OpenScope(ctx, v.services)
}
func (h *host) Ready() bool { return h.ready.Load() }
func (h *host) Start(ctx context.Context, modules []module.Module) (err error) {
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
	r := module.NewRegistry()
	for _, m := range modules {
		if err = r.Register(m); err != nil {
			return err
		}
	}
	plan, err := r.Plan()
	if err != nil {
		return err
	}
	providers, _ := di.NewRegistry(h.options.CleanupTimeout)
	b := &builder{Builder: providers, defaults: map[string]string{}}
	for _, phase := range []struct {
		name string
		hook func(module.Module) func(module.Builder) error
	}{
		{"preconfigure", func(m module.Module) func(module.Builder) error { return m.PreConfigure }},
		{"configure", func(m module.Module) func(module.Builder) error { return m.Configure }},
		{"postconfigure", func(m module.Module) func(module.Builder) error { return m.PostConfigure }},
	} {
		for _, m := range plan.Ordered {
			if err = ctx.Err(); err != nil {
				return err
			}
			if hook := phase.hook(m); hook != nil {
				if err = hook(b); err != nil {
					return fmt.Errorf("%s %s: %w", phase.name, m.Name, err)
				}
			}
		}
	}
	sources := append([]config.Source(nil), h.sources...)
	if h.reader, err = config.NewLoader(config.StartupRules()).Load(h.mergeDefaults(sources, b.defaults)); err != nil {
		return err
	}
	h.container, err = providers.Seal(ctx)
	if err != nil {
		return err
	}
	if err = h.runStartupChecks(ctx); err != nil {
		return err
	}
	if err = h.checkScopeKinds(ctx, plan.Ordered); err != nil {
		return err
	}
	for _, m := range plan.Ordered {
		if err = ctx.Err(); err != nil {
			return err
		}
		if m.Start != nil {
			if err = m.Start(ctx, view{h.container, h.reader}); err != nil {
				return fmt.Errorf("start %s: %w", m.Name, err)
			}
		}
		h.started = append(h.started, m)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	h.ready.Store(true)
	return nil
}
func (h *host) cleanup(ctx context.Context) error {
	h.ready.Store(false)
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.options.CleanupTimeout)
	defer cancel()
	var err error
	for i := len(h.started) - 1; i >= 0; i-- {
		m := h.started[i]
		if m.Stop != nil {
			err = errors.Join(err, m.Stop(cleanup, view{h.container, h.reader}))
		}
	}
	h.started = nil
	for i := len(h.rootStarted) - 1; i >= 0; i-- {
		e := h.rootStarted[i]
		if e.stop != nil {
			err = errors.Join(err, e.stop(cleanup, view{e.services, e.reader}))
		}
	}
	h.rootStarted = nil
	if h.container != nil {
		err = errors.Join(err, h.container.Close(cleanup))
		h.container = nil
	}
	return err
}

// mergeDefaults merges module defaults into the defaults layer; a caller
// defaults source keeps its override precedence within the layer.
func (h *host) mergeDefaults(sources []config.Source, defaults map[string]string) []config.Source {
	found := false
	for i, s := range sources {
		if s.Name != "defaults" {
			continue
		}
		v := map[string]string{}
		for k, x := range defaults {
			v[k] = x
		}
		for k, x := range s.Values {
			v[k] = x
		}
		sources[i].Values = v
		found = true
	}
	if !found {
		sources = append(sources, config.Source{Name: "defaults", Values: defaults})
	}
	return sources
}

func (h *host) readerWithDefaults(defaults map[string]string) (config.Reader, error) {
	return config.NewLoader(config.StartupRules()).Load(h.mergeDefaults(append([]config.Source(nil), h.sources...), defaults))
}

func (h *host) runStartupChecks(ctx context.Context) error {
	// Check all declared capabilities before any module can accept work.
	for _, check := range h.options.StartupChecks {
		if err := check(ctx, view{h.container, h.reader}); err != nil {
			return fmt.Errorf("startup compatibility: %w", err)
		}
	}
	return nil
}

func (h *host) checkScopeKinds(ctx context.Context, ordered []module.Module) error {
	for _, m := range ordered {
		if len(m.RequiredScopeKinds) == 0 {
			continue
		}
		value, resolveErr := h.container.Resolve(ctx, scope.RegistryKey)
		if resolveErr != nil {
			return fmt.Errorf("module %s scope registry: %w", m.Name, resolveErr)
		}
		registry, ok := value.(*scope.Registry)
		if !ok || registry == nil {
			return fmt.Errorf("invalid scope registry")
		}
		if err := registry.Require(m.RequiredScopeKinds...); err != nil {
			return fmt.Errorf("module %s: %w", m.Name, err)
		}
	}
	return nil
}
func (h *host) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx == nil {
		return fmt.Errorf("runtime context required")
	}
	if h.stopped {
		return nil
	}
	h.stopped = true
	return h.shutdown(ctx)
}
func (h *host) shutdown(ctx context.Context) error {
	h.ready.Store(false)
	drain, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.options.DrainTimeout)
	defer cancel()
	var err error
	for _, p := range h.participants {
		err = errors.Join(err, p.Quiesce(drain))
	}
	for _, p := range h.participants {
		err = errors.Join(err, p.Drain(drain))
	}
	if err != nil || drain.Err() != nil {
		err = errors.Join(err, drain.Err())
		for _, p := range h.participants {
			p.Cancel()
		}
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), h.options.CleanupTimeout)
		for _, p := range h.participants {
			err = errors.Join(err, p.Drain(cleanup))
		}
		done()
	}
	return errors.Join(err, h.cleanup(ctx))
}
