package module_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/kernel/module"
	"github.com/cymondez/pangolin-framework/kernel/runtime"
)

// ---- end-to-end fixtures used through Composition/StartRoot ----

type rootPrivStoreI interface {
	Find(id string) (string, bool)
}

var rootPrivState struct {
	startStoreMarker    string
	startUseCaseMarker  string
	startIdentityFailed bool
	stopStoreMarker     string
	startErr            error
	openScopeMarkers    []string
	scopeCleanupCount   int
}

type rootPrivStore struct{ marker string }

func newRootPrivStore() *rootPrivStore { return &rootPrivStore{marker: "root-local"} }

func (s *rootPrivStore) Find(id string) (string, bool) { return s.marker, true }

type rootPrivUsecase struct{ Store rootPrivStoreI }

func newRootPrivUsecase(s rootPrivStoreI) *rootPrivUsecase { return &rootPrivUsecase{Store: s} }

// Scoped variant with cleanup observation.
type rootPrivScopedStore struct{}

func (s *rootPrivScopedStore) Find(id string) (string, bool) { return "root-scoped", true }

func newRootPrivScopedStore() (*rootPrivScopedStore, func(context.Context) error, error) {
	return &rootPrivScopedStore{}, func(context.Context) error {
		rootPrivState.scopeCleanupCount++
		return nil
	}, nil
}

type rootPrivScopedUsecase struct{ Store rootPrivStoreI }

func newRootPrivScopedUsecase(s rootPrivStoreI) *rootPrivScopedUsecase {
	return &rootPrivScopedUsecase{Store: s}
}

func newRootTestHost(t *testing.T) runtime.Host {
	t.Helper()
	host, err := runtime.New(runtime.Options{DrainTimeout: time.Second, CleanupTimeout: time.Second}, []config.Source{
		{Name: "defaults", Values: map[string]string{
			"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s",
			"uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return host
}

type rootPrivAppModule struct{ probe bool }

func (m *rootPrivAppModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "root-priv-app"}
}

func (m *rootPrivAppModule) Configure(b module.RootBuilder) error {
	s := b.Services()
	if err := s.Add(newRootPrivStore, module.Singleton()); err != nil {
		return err
	}
	return s.Add(newRootPrivUsecase)
}

func (m *rootPrivAppModule) Start(ctx context.Context, rt module.Runtime) error {
	store, err := rt.Services().Resolve(ctx, module.Contract[rootPrivStoreI]())
	if err != nil {
		rootPrivState.startErr = err
		return err
	}
	ucV, err := rt.Services().Resolve(ctx, module.Service[*rootPrivUsecase]())
	if err != nil {
		rootPrivState.startErr = err
		return err
	}
	uc := ucV.(*rootPrivUsecase)
	rootPrivState.startStoreMarker = store.(*rootPrivStore).marker
	rootPrivState.startUseCaseMarker = uc.Store.(*rootPrivStore).marker
	if uc.Store != store {
		// The store is a singleton: the usecase must receive the same instance.
		rootPrivState.startIdentityFailed = true
	}
	return nil
}

func (m *rootPrivAppModule) Stop(ctx context.Context, rt module.Runtime) error {
	store, err := rt.Services().Resolve(ctx, module.Contract[rootPrivStoreI]())
	if err != nil {
		return err
	}
	rootPrivState.stopStoreMarker = store.(*rootPrivStore).marker
	return nil
}

func TestPrivateRootRuntimeStartStopResolution(t *testing.T) {
	rootPrivState = struct {
		startStoreMarker    string
		startUseCaseMarker  string
		startIdentityFailed bool
		stopStoreMarker     string
		startErr            error
		openScopeMarkers    []string
		scopeCleanupCount   int
	}{}
	app := &rootPrivAppModule{}
	host := newRootTestHost(t)
	if err := runtime.StartRoot(context.Background(), host, app); err != nil {
		t.Fatalf("StartRoot with owner-private inference: %v", err)
	}
	if !host.Ready() {
		t.Fatal("host not ready")
	}
	if rootPrivState.startIdentityFailed {
		t.Fatal("singleton store identity broken between direct and usecase resolution")
	}
	if rootPrivState.startStoreMarker != "root-local" || rootPrivState.startUseCaseMarker != "root-local" {
		t.Fatalf("start resolution drifted: %+v", rootPrivState)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := host.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if rootPrivState.stopStoreMarker != "root-local" {
		t.Fatalf("stop resolution drifted: %q", rootPrivState.stopStoreMarker)
	}
}

type rootPrivScopedModule struct{ probe bool }

func (m *rootPrivScopedModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "root-priv-scoped"}
}

func (m *rootPrivScopedModule) Configure(b module.RootBuilder) error {
	s := b.Services()
	if err := s.Add(newRootPrivScopedStore, module.Operation()); err != nil {
		return err
	}
	return s.Add(newRootPrivScopedUsecase, module.Operation())
}

func (m *rootPrivScopedModule) Start(ctx context.Context, rt module.Runtime) error {
	scope, err := module.OpenScope(ctx, rt.Services())
	if err != nil {
		return err
	}
	store, err := scope.Resolve(ctx, module.Contract[rootPrivStoreI]())
	if err != nil {
		_ = scope.Close(ctx)
		return err
	}
	use, err := scope.Resolve(ctx, module.Service[*rootPrivScopedUsecase]())
	if err != nil {
		_ = scope.Close(ctx)
		return err
	}
	if use.(*rootPrivScopedUsecase).Store != store {
		_ = scope.Close(ctx)
		return errors.New("scoped operation identity broken")
	}
	if marker, _ := store.(*rootPrivScopedStore).Find(""); marker != "root-scoped" {
		return errors.New("scoped store returned an unexpected marker")
	}
	rootPrivState.openScopeMarkers = append(rootPrivState.openScopeMarkers, "root-scoped")
	if err := scope.Close(ctx); err != nil {
		return err
	}
	return nil
}

func (m *rootPrivScopedModule) Stop(ctx context.Context, rt module.Runtime) error {
	scope, err := module.OpenScope(ctx, rt.Services())
	if err != nil {
		return err
	}
	if _, err := scope.Resolve(ctx, module.Contract[rootPrivStoreI]()); err != nil {
		_ = scope.Close(ctx)
		return err
	}
	return scope.Close(ctx)
}

func TestPrivateRootRuntimeOpenScopeOperation(t *testing.T) {
	rootPrivState = struct {
		startStoreMarker    string
		startUseCaseMarker  string
		startIdentityFailed bool
		stopStoreMarker     string
		startErr            error
		openScopeMarkers    []string
		scopeCleanupCount   int
	}{}
	host := newRootTestHost(t)
	if err := runtime.StartRoot(context.Background(), host, &rootPrivScopedModule{}); err != nil {
		t.Fatalf("StartRoot with scoped private resolution: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := host.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if len(rootPrivState.openScopeMarkers) != 1 || rootPrivState.openScopeMarkers[0] != "root-scoped" {
		t.Fatalf("open scope markers drifted: %v", rootPrivState.openScopeMarkers)
	}
	if rootPrivState.scopeCleanupCount != 2 {
		t.Fatalf("each operation scope must dispose its private resource once, ran %d", rootPrivState.scopeCleanupCount)
	}
}

// A dependent module must not resolve the provider's private alias, even with
// a direct dependency; StartRoot fails and reclaims resources.
type rootPrivLibModule struct{ probe bool }

func (m *rootPrivLibModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "root-priv-lib"}
}

func (m *rootPrivLibModule) Configure(b module.RootBuilder) error {
	s := b.Services()
	if err := s.Add(newRootPrivStore, module.Singleton()); err != nil {
		return err
	}
	return s.Add(newRootPrivUsecase)
}

type rootPrivClientModule struct {
	probe   bool
	started bool
}

func (m *rootPrivClientModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "root-priv-client"}
}

func (m *rootPrivClientModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rootPrivLibModule]()}
}

func (m *rootPrivClientModule) Start(ctx context.Context, rt module.Runtime) error {
	_, err := rt.Services().Resolve(ctx, module.Contract[rootPrivStoreI]())
	if err != nil {
		return err
	}
	m.started = true
	return nil
}

func TestPrivateRootRuntimePrivateAliasHiddenFromDependent(t *testing.T) {
	lib := &rootPrivLibModule{}
	client := &rootPrivClientModule{}
	host := newRootTestHost(t)
	err := runtime.StartRoot(context.Background(), host, client, lib)
	if err == nil {
		t.Fatal("the dependent must not resolve the provider's private alias")
	}
	if !strings.Contains(err.Error(), "root-priv-client") || !strings.Contains(err.Error(), "missing provider") {
		t.Fatalf("unexpected failure diagnostics: %v", err)
	}
	if client.started {
		t.Fatal("the failed Start must not record dependent progress")
	}
}

// Two modules reuse the same Go interface privately; each Start resolves only
// its own implementation.
type rootPrivLibStore struct{ marker string }

func (s *rootPrivLibStore) Find(id string) (string, bool) { return s.marker, true }

func newRootPrivLibStore() *rootPrivLibStore { return &rootPrivLibStore{marker: "lib-store"} }

type rootPrivSharedLibUsecase struct{ Store rootPrivStoreI }

func newRootPrivSharedLibUsecase(s rootPrivStoreI) *rootPrivSharedLibUsecase {
	return &rootPrivSharedLibUsecase{Store: s}
}

type rootPrivSharedClientUsecase struct{ Store rootPrivStoreI }

func newRootPrivSharedClientUsecase(s rootPrivStoreI) *rootPrivSharedClientUsecase {
	return &rootPrivSharedClientUsecase{Store: s}
}

type rootPrivClientStore struct{ marker string }

func (s *rootPrivClientStore) Find(id string) (string, bool) { return s.marker, true }

func newRootPrivClientStore() *rootPrivClientStore {
	return &rootPrivClientStore{marker: "client-store"}
}

var rootPrivSeenMarkers []string

type rootPrivSharedLibModule struct{ probe bool }

func (m *rootPrivSharedLibModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "root-priv-shared-lib"}
}

func (m *rootPrivSharedLibModule) Configure(b module.RootBuilder) error {
	s := b.Services()
	if err := s.Add(newRootPrivLibStore, module.Singleton()); err != nil {
		return err
	}
	return s.Add(newRootPrivSharedLibUsecase)
}

func (m *rootPrivSharedLibModule) Start(ctx context.Context, rt module.Runtime) error {
	use, err := rt.Services().Resolve(ctx, module.Service[*rootPrivSharedLibUsecase]())
	if err != nil {
		return err
	}
	if marker, _ := use.(*rootPrivSharedLibUsecase).Store.(*rootPrivLibStore).Find(""); marker != "lib-store" {
		return errors.New("lib private resolution drifted: " + marker)
	}
	rootPrivSeenMarkers = append(rootPrivSeenMarkers, "lib")
	return nil
}

type rootPrivSharedClientModule struct{ probe bool }

func (m *rootPrivSharedClientModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "root-priv-shared-client"}
}

func (m *rootPrivSharedClientModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rootPrivSharedLibModule]()}
}

func (m *rootPrivSharedClientModule) Configure(b module.RootBuilder) error {
	s := b.Services()
	if err := s.Add(newRootPrivClientStore, module.Singleton()); err != nil {
		return err
	}
	return s.Add(newRootPrivSharedClientUsecase)
}

func (m *rootPrivSharedClientModule) Start(ctx context.Context, rt module.Runtime) error {
	use, err := rt.Services().Resolve(ctx, module.Service[*rootPrivSharedClientUsecase]())
	if err != nil {
		return err
	}
	if marker, _ := use.(*rootPrivSharedClientUsecase).Store.(*rootPrivClientStore).Find(""); marker != "client-store" {
		return errors.New("client private resolution drifted: " + marker)
	}
	rootPrivSeenMarkers = append(rootPrivSeenMarkers, "client")
	return nil
}

func TestPrivateRootRuntimeSharedInterfaceIndependent(t *testing.T) {
	rootPrivSeenMarkers = nil
	host := newRootTestHost(t)
	if err := runtime.StartRoot(context.Background(), host, &rootPrivSharedClientModule{}, &rootPrivSharedLibModule{}); err != nil {
		t.Fatalf("StartRoot with two private users of the same interface: %v", err)
	}
	if len(rootPrivSeenMarkers) != 2 {
		t.Fatalf("expected both modules to resolve their own usecase, got %v", rootPrivSeenMarkers)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := host.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}
