package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/kernel/di"
	"github.com/cymondez/pangolin-framework/kernel/module"
)

func settings() []config.Source {
	return []config.Source{{Name: "defaults", Values: map[string]string{"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s", "uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4"}}}
}
func makeHost(t *testing.T, p ...Participant) Host {
	t.Helper()
	h, e := New(Options{time.Second, time.Second}, settings(), p...)
	if e != nil {
		t.Fatal(e)
	}
	return h
}
func TestA02(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "phase-order"
		if fail {
			name = "partial-start-failure"
		}
		t.Run(name, func(t *testing.T) {
			h := makeHost(t)
			order := []string{}
			modules := []module.Module{}
			for _, name := range []string{"c", "b", "a"} {
				m := module.Module{Name: name, Version: "0.1.0"}
				if name != "a" {
					m.DependsOn = []module.Dependency{{Name: "a", VersionConstraint: "=0.1.0"}}
				}
				m.PreConfigure = func(module.Builder) error { order = append(order, "pre:"+name); return nil }
				m.Configure = func(module.Builder) error { order = append(order, "configure:"+name); return nil }
				m.PostConfigure = func(module.Builder) error { order = append(order, "post:"+name); return nil }
				m.Start = func(ctx context.Context, r module.Runtime) error {
					if h.Ready() {
						t.Fatal("early ready")
					}
					if r.Services() == nil || r.Config() == nil {
						t.Fatal("runtime view")
					}
					order = append(order, "start:"+name)
					if fail && name == "c" {
						order = append(order, "partial:c")
						return errors.New("start failure")
					}
					return nil
				}
				m.Stop = func(context.Context, module.Runtime) error { order = append(order, "stop:"+name); return nil }
				modules = append(modules, m)
			}
			e := h.Start(context.Background(), modules)
			if (e != nil) != fail || h.Ready() == fail {
				t.Fatal(e, h.Ready())
			}
			if !fail {
				if e = h.Stop(context.Background()); e != nil {
					t.Fatal(e)
				}
			}
			want := []string{"pre:a", "pre:b", "pre:c", "configure:a", "configure:b", "configure:c", "post:a", "post:b", "post:c", "start:a", "start:b", "start:c"}
			if fail {
				want = append(want, "partial:c", "stop:b", "stop:a")
			} else {
				want = append(want, "stop:c", "stop:b", "stop:a")
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatal(order)
			}
			_ = h.Stop(context.Background())
		})
	}
}
func TestA15(t *testing.T) {
	for _, kind := range []string{"active-http", "active-worker", "drain-timeout"} {
		t.Run(kind, func(t *testing.T) {
			g := NewWorkGroup()
			h, e := New(Options{30 * time.Millisecond, time.Second}, settings(), g)
			if e != nil {
				t.Fatal(e)
			}
			dependencyAlive := true
			stopped := make(chan struct{})
			m := module.Module{Name: "db", Version: "0.1.0", Stop: func(context.Context, module.Runtime) error { dependencyAlive = false; close(stopped); return nil }}
			if e = h.Start(context.Background(), []module.Module{m}); e != nil {
				t.Fatal(e)
			}
			ctx, done, e := g.Enter(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			finished := make(chan error, 1)
			go func() { finished <- h.Stop(context.Background()) }()
			// Quiesce is the barrier: no new work enters after it closes admission.
			for {
				g.mu.Lock()
				closed := g.closed
				g.mu.Unlock()
				if closed {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if h.Ready() {
				t.Fatal("ready during drain")
			}
			if _, _, e := g.Enter(context.Background()); e == nil {
				t.Fatal("admitted after quiesce")
			}
			select {
			case <-stopped:
				t.Fatal("dependency closed early")
			default:
			}
			if !dependencyAlive {
				t.Fatal("dependency unavailable")
			}
			if kind == "drain-timeout" {
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
					t.Fatal("work not cancelled")
				}
			}
			done()
			done()
			e = <-finished
			if (e != nil) != (kind == "drain-timeout") {
				t.Fatal(e)
			}
			if dependencyAlive {
				t.Fatal("dependency not closed")
			}
			if e = h.Stop(context.Background()); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestHostFailureGuards(t *testing.T) {
	if _, e := New(Options{}, nil); e == nil {
		t.Fatal("invalid options")
	}
	if _, e := New(Options{time.Second, time.Second}, nil, nil); e == nil {
		t.Fatal("nil participant")
	}
	for _, failure := range []string{"graph", "pre", "configure", "post", "config", "seal", "panic", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			h := makeHost(t)
			m := module.Module{Name: "a", Version: "0.1.0"}
			cause := errors.New("injected")
			bad := func(module.Builder) error { return cause }
			switch failure {
			case "graph":
				m.Version = "bad"
			case "pre":
				m.PreConfigure = bad
			case "configure":
				m.Configure = bad
			case "post":
				m.PostConfigure = bad
			case "config":
				h, _ = New(Options{time.Second, time.Second}, nil)
			case "seal":
				m.Configure = func(b module.Builder) error {
					return b.Provide(di.Provider{Key: di.NewKey[int]("a"), Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return nil, nil, cause }})
				}
			case "panic":
				m.Start = func(context.Context, module.Runtime) error { panic(cause) }
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "cancel" {
				cancel()
			}
			if failure == "panic" {
				func() {
					defer func() {
						if recover() != cause {
							t.Fatal("panic lost")
						}
					}()
					_ = h.Start(ctx, []module.Module{m})
				}()
			} else {
				if e := h.Start(ctx, []module.Module{m}); e == nil {
					t.Fatal("failure lost")
				}
			}
			if h.Ready() {
				t.Fatal("ready on failure")
			}
			if e := h.Start(ctx, nil); e == nil {
				t.Fatal("restart")
			}
			if e := h.Stop(nil); e == nil {
				t.Fatal("nil stop")
			}
		})
	}
	h := makeHost(t)
	m := module.Module{Name: "a", Version: "0.1.0", Configure: func(b module.Builder) error {
		if b.SetDefault("", "x") == nil {
			t.Fatal("empty")
		}
		if e := b.SetDefault("custom", "x"); e != nil {
			return e
		}
		if b.SetDefault("custom", "y") == nil {
			t.Fatal("duplicate")
		}
		return nil
	}}
	if e := h.Start(context.Background(), []module.Module{m}); e != nil {
		t.Fatal(e)
	}
	_ = h.Stop(context.Background())
	g := NewWorkGroup()
	if _, _, e := g.Enter(nil); e == nil {
		t.Fatal("nil enter")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := g.Enter(ctx); e == nil {
		t.Fatal("cancelled enter")
	}
	if e := g.Drain(context.Background()); e != nil {
		t.Fatal(e)
	}
}

func TestStartFailureDrainsPreviouslyStartedWorker(t *testing.T) {
	g := NewWorkGroup()
	h := makeHost(t, g)
	doneWork := make(chan func(), 1)
	release := make(chan struct{})
	stopped := false
	modules := []module.Module{
		{Name: "a-worker", Version: "0.1.0", Start: func(context.Context, module.Runtime) error {
			_, done, e := g.Enter(context.Background())
			if e != nil {
				return e
			}
			doneWork <- done
			go func() { <-release; done() }()
			return nil
		}, Stop: func(context.Context, module.Runtime) error { stopped = true; return nil }},
		{Name: "z-failure", Version: "0.1.0", Start: func(context.Context, module.Runtime) error { return errors.New("start failure") }},
	}
	result := make(chan error, 1)
	go func() { result <- h.Start(context.Background(), modules) }()
	<-doneWork
	for {
		g.mu.Lock()
		closed := g.closed
		g.mu.Unlock()
		if closed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-result:
		t.Fatal("closed resources before worker drain")
	default:
	}
	close(release)
	if e := <-result; e == nil || !stopped {
		t.Fatal(e, stopped)
	}
}
