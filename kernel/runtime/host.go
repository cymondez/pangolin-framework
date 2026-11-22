package runtime

import (
	"context"
	"errors"
	"fmt"
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
func (v view) Config() config.Reader { return v.reader }
func (h *host) Ready() bool          { return h.ready.Load() }
func (h *host) Start(ctx context.Context, modules []module.Module) (err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx == nil || h.attempted || h.stopped {
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
	found := false
	for i, s := range sources {
		if s.Name == "defaults" {
			v := map[string]string{}
			for k, x := range b.defaults {
				v[k] = x
			}
			for k, x := range s.Values {
				v[k] = x
			}
			sources[i].Values = v
			found = true
		}
	}
	if !found {
		sources = append(sources, config.Source{Name: "defaults", Values: b.defaults})
	}
	h.reader, err = config.NewLoader(config.StartupRules()).Load(sources)
	if err != nil {
		return err
	}
	h.container, err = providers.Seal(ctx)
	if err != nil {
		return err
	}
	// Check all declared capabilities before any module can accept work.
	for _, m := range plan.Ordered {
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
		if err = registry.Require(m.RequiredScopeKinds...); err != nil {
			return fmt.Errorf("module %s: %w", m.Name, err)
		}
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
	if h.container != nil {
		err = errors.Join(err, h.container.Close(cleanup))
		h.container = nil
	}
	return err
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
