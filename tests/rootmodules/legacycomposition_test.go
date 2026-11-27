// Reproduction of the pre-fix generated-app composition shape: the
// application root module owns every framework resource under custom tokens
// and only DependsOn the UserRoot aggregator, so ordinary generated modules
// cannot receive framework services through typed constructor injection. The
// engine rejects the registration at seal; the only way the engine would
// accept it is a direct Type edge to the provider owner, which for generated
// modules means importing the app root package and creating a package cycle
// (app -> modules registry -> user module -> app). The foundation module
// package introduced with this fix removes the contradiction: user modules
// depend on the foundation Type, not on the app root.
package rootmodules

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/di"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
)

// legacyResource is a construction-only stand-in for the UOW backend; the
// failing compositions below never run any factory (seal validates
// visibility before provisioning), so Begin is unreachable.
type legacyResource struct{}

func (legacyResource) Begin(context.Context, uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return nil, nil, uow.ErrRequired
}

func newLegacyManager() uow.UnitOfWorkManager {
	manager, err := uow.NewManager(legacyResource{}, uow.DefaultOptions{DefaultTimeout: 5 * time.Second, CleanupTimeout: 2 * time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 4})
	if err != nil {
		panic(err)
	}
	return manager
}

// legacyResources mirrors the appResources bundle the pre-fix generated root
// owned: framework services nested inside one opaque composition value.
type legacyResources struct{ Manager uow.UnitOfWorkManager }

var (
	legacyBundleToken  = di.NewKey[*legacyResources]("app.composition")
	legacyThingToken   = di.NewKey[*legacyThing]("app.legacy-thing")
	legacyManagerToken = di.NewKey[uow.UnitOfWorkManager]("app.uow")
)

// legacyThing and legacyService are ordinary user-module constructors: the
// framework services are requested strictly by framework types, without
// low-level tokens, dependency declarations or owner knowledge; registration
// is a plain Add, exactly what `module add` instructs.
type legacyThing struct {
	Manager uow.UnitOfWorkManager
}

func NewLegacyThing(manager uow.UnitOfWorkManager) *legacyThing {
	return &legacyThing{Manager: manager}
}

type legacyService struct {
	Scopes *scope.Registry
}

func NewLegacyService(sc *scope.Registry) *legacyService {
	return &legacyService{Scopes: sc}
}

type legacyUserRoot[T any] struct{}

func (legacyUserRoot[T]) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "user-modules"}
}

func (legacyUserRoot[T]) DependsOn() []module.Ref {
	return []module.Ref{module.Of[T]()}
}

type legacyUserModule struct{}

func (legacyUserModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "legacyuser"}
}

func (legacyUserModule) Configure(b module.RootBuilder) error {
	// Exactly what `module add` instructs today: return b.Services().Add(NewService).
	return b.Services().Add(NewLegacyThing)
}

type legacyUserServiceModule struct{}

func (legacyUserServiceModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "legacyuser"}
}

func (legacyUserServiceModule) Configure(b module.RootBuilder) error {
	return b.Services().Add(NewLegacyService)
}

type legacyRootModule struct{}

func (legacyRootModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "app", Version: "0.1.0"}
}

// DependsOn mirrors the pre-fix generated root: only the UserRoot aggregator.
func (legacyRootModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[legacyUserRoot[legacyUserModule]]()}
}

func (legacyRootModule) Configure(b module.RootBuilder) error {
	manager := newLegacyManager()
	if err := b.Provide(di.Provider{Key: legacyBundleToken, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		return &legacyResources{Manager: manager}, nil, nil
	}}); err != nil {
		return err
	}
	// The scope registry additionally appears under the framework's custom
	// startup-check token, exactly like the pre-fix template.
	if err := b.Provide(di.Provider{Key: scope.RegistryKey, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
			Authority:  legacyAuthority{},
			Permission: func(context.Context, scope.ScopeBinding, string) error { return scope.ErrDenied },
		})
		if err != nil {
			return nil, nil, err
		}
		return registry, nil, nil
	}}); err != nil {
		return err
	}
	return nil
}

type legacyAuthority struct{}

func (legacyAuthority) Check(context.Context, scope.PrincipalRef, string) (string, error) {
	return "", scope.ErrDenied
}

// runLegacyComposition starts the pre-fix shape and returns the engine
// diagnostics: factories of the failing graph are never provisioned, so no
// backend or database is required.
func runLegacyComposition(t *testing.T, host runtime.Host, root any) error {
	t.Helper()
	err := runtime.StartRoot(context.Background(), host, root)
	if err == nil {
		host.Stop(context.Background())
		t.Fatal("legacy composition unexpectedly resolved user-module framework injection")
	}
	return err
}

func newLegacyTestHost(t *testing.T) runtime.Host {
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

func TestLegacyRootOwnedServicesRejectTypedInjection(t *testing.T) {
	err := runLegacyComposition(t, newLegacyTestHost(t), &legacyRootModule{})
	if !strings.Contains(err.Error(), "missing provider github.com/pangolin-framework/pangolin/persistence/uow.UnitOfWorkManager") {
		t.Fatalf("expected missing UOW contract provider diagnostic:\n%v", err)
	}
	if !strings.Contains(err.Error(), "module legacyuser") {
		t.Fatalf("diagnostic must name the user module consumer: %v", err)
	}
}

// The scope registry is registered only under the framework's custom
// startup-check token, so the canonical *scope.Registry constructor parameter
// cannot resolve either; the engine reports one violating dependency at a
// time, in sorted token order, hence a dedicated composition.
func TestLegacyRootOwnedServicesRejectRegistryTypedInjection(t *testing.T) {
	err := runLegacyComposition(t, newLegacyTestHost(t), &legacyRegistryRootModule{})
	if !strings.Contains(err.Error(), "missing provider *github.com/pangolin-framework/pangolin/security/scope.Registry") {
		t.Fatalf("expected missing scope registry diagnostic:\n%v", err)
	}
	if !strings.Contains(err.Error(), "module legacyuser") {
		t.Fatalf("diagnostic must name the user module consumer: %v", err)
	}
}

// legacyRegistryRootModule keeps the identical pre-fix root shape but routes
// a user module whose constructor takes only *scope.Registry: the registry is
// registered only under the custom startup-check token in this shape.
type legacyRegistryRootModule struct{}

func (legacyRegistryRootModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "app", Version: "0.1.0"}
}

func (legacyRegistryRootModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[legacyUserRoot[legacyUserServiceModule]]()}
}

func (legacyRegistryRootModule) Configure(b module.RootBuilder) error {
	if err := b.Provide(di.Provider{Key: legacyBundleToken, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		return &legacyResources{Manager: newLegacyManager()}, nil, nil
	}}); err != nil {
		return err
	}
	if err := b.Provide(di.Provider{Key: scope.RegistryKey, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
			Authority:  legacyAuthority{},
			Permission: func(context.Context, scope.ScopeBinding, string) error { return scope.ErrDenied },
		})
		if err != nil {
			return nil, nil, err
		}
		return registry, nil, nil
	}}); err != nil {
		return err
	}
	return nil
}

// legacyExportedRootModule additionally publishes the manager and registry
// under canonical framework identities owned by the app root, the closest
// possible root-side preparation; the user module still declares no edge to
// it, because generated modules cannot import the app root package.
type legacyExportedRootModule struct{}

func (legacyExportedRootModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "app", Version: "0.1.0"}
}

func (legacyExportedRootModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[legacyUserRoot[legacyUserModule]]()}
}

func (legacyExportedRootModule) Configure(b module.RootBuilder) error {
	manager := newLegacyManager()
	if err := b.Provide(di.Provider{Key: module.Contract[uow.UnitOfWorkManager](), Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		return manager, nil, nil
	}}); err != nil {
		return err
	}
	registryToken := module.Service[*scope.Registry]()
	if err := b.Provide(di.Provider{Key: registryToken, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
			Authority:  legacyAuthority{},
			Permission: func(context.Context, scope.ScopeBinding, string) error { return scope.ErrDenied },
		})
		if err != nil {
			return nil, nil, err
		}
		return registry, nil, nil
	}}); err != nil {
		return err
	}
	if err := b.Provide(di.Provider{Key: scope.RegistryKey, Lifetime: di.Singleton, DependsOn: []di.Token{registryToken}, Factory: func(ctx context.Context, resolve di.Resolver) (any, di.Cleanup, error) {
		value, err := resolve.Resolve(ctx, registryToken)
		if err != nil {
			return nil, nil, err
		}
		return value.(*scope.Registry), nil, nil
	}}); err != nil {
		return err
	}
	if err := b.Services().Export(module.Contract[uow.UnitOfWorkManager]()); err != nil {
		return err
	}
	return b.Services().Export(registryToken)
}

func TestLegacyExportedRootServicesStillNeedOwnerEdge(t *testing.T) {
	err := runLegacyComposition(t, newLegacyTestHost(t), &legacyExportedRootModule{})
	// The engine reports the first violating dependency of the user ctor in
	// sorted token order; the diagnostic names consumer, type and owner.
	message := "module legacyuser cannot resolve github.com/pangolin-framework/pangolin/persistence/uow.UnitOfWorkManager: it is owned by module app; declare a direct dependency on app and require it to export"
	if !strings.Contains(err.Error(), message) {
		t.Fatalf("expected owner-edge diagnostic:\n%v", err)
	}
	if strings.Contains(err.Error(), "missing provider") {
		t.Fatalf("the root exports the contracts; only the owner edge is missing:\n%v", err)
	}
}
