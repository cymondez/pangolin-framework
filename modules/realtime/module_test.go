package realtime

// Coverage tests for the optional Root contract owner (module.go), see
// tests/contract/m7.json scenario mapping. They exercise real root assembly
// through kernel/runtime.StartRoot, including Start/Stop lifecycle, ingress
// cleanup and constructor failures.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/events/outbox"
	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
)

type moduleFoundation struct{ tag string }

func (*moduleFoundation) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "module-foundation"}
}

func (f *moduleFoundation) Configure(b module.RootBuilder) error {
	if err := b.Services().Add(func() *moduleFoundation { return f }, module.Singleton()); err != nil {
		return err
	}
	return b.Services().Export(module.Service[*moduleFoundation]())
}

type moduleTestRoot struct {
	gateway   Gateway
	publisher Publisher
	mu        sync.Mutex
}

func (*moduleTestRoot) ModuleMetadata() module.Metadata { return module.Metadata{Name: "module-root"} }

func (*moduleTestRoot) DependsOn() []module.Ref { return []module.Ref{module.Of[Module]()} }

func (r *moduleTestRoot) Start(ctx context.Context, rt module.Runtime) error {
	gateway, err := rt.Services().Resolve(ctx, module.Contract[Gateway]())
	if err != nil {
		return err
	}
	publisher, err := rt.Services().Resolve(ctx, module.Contract[Publisher]())
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gateway = gateway.(Gateway)
	r.publisher = publisher.(Publisher)
	return nil
}

func (*moduleTestRoot) Stop(context.Context, module.Runtime) error { return nil }

func newModuleHub(tag string, publisher *outbox.Publisher) (*Hub, error) {
	authority := &currentAuthority{}
	pipeline, err := identity.NewPipeline(authority)
	if err != nil {
		return nil, err
	}
	resolver, err := tenancy.NewResolver(identity.Identity, authority)
	if err != nil {
		return nil, err
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
		Authority: authority, Permission: authority.permission,
	})
	if err != nil {
		return nil, err
	}
	return New(Options{
		Producer: tag, Connect: Access{Permission: "connect"},
		Channels: []Channel{{Name: "updates", Target: "Update", Kinds: []string{"tenant"},
			Publish: Access{Permission: "publish"}, Subscribe: Access{Permission: "subscribe"},
			Receive: Access{Permission: "receive"}}},
		MaxConnections: 4, MaxSubscriptions: 2, QueueSize: 1, MaxPayload: 1024, MaxOperations: 2,
		OperationTimeout: time.Second,
	}, pipeline, resolver, registry, publisher)
}

func newModuleRuntime(t *testing.T) runtime.Host {
	t.Helper()
	host, err := runtime.New(runtime.Options{DrainTimeout: time.Second, CleanupTimeout: time.Second}, []config.Source{{Name: "defaults", Values: map[string]string{
		"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s", "uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func TestRealtimeModuleLifecycle(t *testing.T) {
	cleanupCalled := atomic.Bool{}
	owner := &moduleTestRoot{}
	foundation := &moduleFoundation{tag: "module-realtime"}
	tag := "module-realtime"
	mod := &Module{
		Dependencies: []module.Ref{module.Of[moduleFoundation]()},
		Constructor: func(f *moduleFoundation) (*Hub, error) {
			if f.tag != tag {
				return nil, errors.New("constructor received the wrong foundation instance")
			}
			return newModuleHub(f.tag, nil)
		},
		StartIngress: func(context.Context, *Hub) (func(context.Context) error, error) {
			return func(context.Context) error { cleanupCalled.Store(true); return nil }, nil
		},
	}
	host := newModuleRuntime(t)
	if err := runtime.StartRoot(context.Background(), host, owner, foundation, mod); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	gateway, publisher := owner.gateway, owner.publisher
	owner.mu.Unlock()
	if gateway == nil || publisher == nil {
		t.Fatal("root start did not resolve exported contracts")
	}
	if gateway.ConnectionLimit() != 4 {
		t.Fatalf("unexpected gateway connection limit %d", gateway.ConnectionLimit())
	}
	// The gateway admits a verified session before any lifecycle drain.
	session, subject, err := openGatewaySession(gateway)
	if session == nil {
		t.Fatalf("gateway session: %v", err)
	}
	if subject.Tenant != "alpha" {
		t.Fatalf("unexpected subject %v", subject)
	}
	session.Close()
	if err := gateway.Quiesce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	gateway.Cancel()
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !cleanupCalled.Load() {
		t.Fatal("ingress cleanup was not invoked")
	}
}

func openGatewaySession(gateway Gateway) (*Session, Subject, error) {
	credential := identity.Credential{Kind: identity.BearerKind, Value: "alpha"}
	subject, err := gateway.Authenticate(context.Background(), credential)
	if err != nil {
		return nil, subject, err
	}
	session, err := gateway.Open(context.Background(), credential, subject)
	return session, subject, err
}

func TestRealtimeModuleStopIngressFailure(t *testing.T) {
	cause := errors.New("ingress stop failed")
	foundation := &moduleFoundation{tag: "module-realtime-stop"}
	mod := &Module{
		Dependencies: []module.Ref{module.Of[moduleFoundation]()},
		Constructor:  func(f *moduleFoundation) (*Hub, error) { return newModuleHub(f.tag, nil) },
		StartIngress: func(context.Context, *Hub) (func(context.Context) error, error) {
			return func(context.Context) error { return cause }, nil
		},
	}
	host := newModuleRuntime(t)
	if err := runtime.StartRoot(context.Background(), host, &moduleTestRoot{}, foundation, mod); err != nil {
		t.Fatal(err)
	}
	err := host.Stop(context.Background())
	if !errors.Is(err, cause) {
		t.Fatalf("ingress stop error was swallowed: %v", err)
	}
}

func TestRealtimeModuleConstructorFailures(t *testing.T) {
	t.Run("nil-constructor", func(t *testing.T) {
		host := newModuleRuntime(t)
		if err := runtime.StartRoot(context.Background(), host, &moduleTestRoot{},
			&moduleFoundation{tag: "module-nil"}, &Module{
				Dependencies: []module.Ref{module.Of[moduleFoundation]()},
				Constructor:  func(f *moduleFoundation) (*Hub, error) { return nil, nil },
			}); err == nil {
			t.Fatal("nil hub constructor was accepted")
		}
	})
	t.Run("configure-rejects-missing-constructor", func(t *testing.T) {
		host := newModuleRuntime(t)
		if err := runtime.StartRoot(context.Background(), host, &moduleTestRoot{},
			&moduleFoundation{tag: "module-none"}, &Module{}); err == nil {
			t.Fatal("module without a constructor was accepted")
		}
	})
	t.Run("start-fails-with-ingress-error", func(t *testing.T) {
		cause := errors.New("ingress start failed")
		host := newModuleRuntime(t)
		err := runtime.StartRoot(context.Background(), host, &moduleTestRoot{},
			&moduleFoundation{tag: "module-ingress"}, &Module{
				Dependencies: []module.Ref{module.Of[moduleFoundation]()},
				Constructor:  func(f *moduleFoundation) (*Hub, error) { return newModuleHub(f.tag, nil) },
				StartIngress: func(context.Context, *Hub) (func(context.Context) error, error) { return nil, cause },
			})
		if !errors.Is(err, cause) {
			t.Fatalf("ingress start error was swallowed: %v", err)
		}
	})
}

func TestRealtimeModuleMetadata(t *testing.T) {
	mod := &Module{Dependencies: []module.Ref{module.Of[moduleFoundation]()}}
	if mod.ModuleMetadata().Name != "realtime" {
		t.Fatalf("unexpected module name %q", mod.ModuleMetadata().Name)
	}
	refs := mod.DependsOn()
	if len(refs) != 1 || refs[0].Type() != module.Of[moduleFoundation]().Type() {
		t.Fatalf("unexpected dependencies %v", refs)
	}
}
