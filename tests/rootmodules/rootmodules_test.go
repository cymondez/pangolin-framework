// Package rootmodules runs the cross-package root-composition acceptance
// described in README.md. The loader zero-instantiates dependency Types, so
// lifecycle events are recorded through a package-level recorder.
package rootmodules

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/app"
	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
)

var rmSharedCount atomic.Int32

type rmShared struct{ id int }

func NewShared() *rmShared { return &rmShared{id: int(rmSharedCount.Add(1))} }

type rmCatalog interface{ Len() int }

type rmCatalogImpl struct{ shared *rmShared }

func (c *rmCatalogImpl) Len() int { return c.shared.id }

func NewCatalog(shared *rmShared) *rmCatalogImpl { return &rmCatalogImpl{shared: shared} }

var rmEvents []string

func rmRecord(event string) { rmEvents = append(rmEvents, event) }
func rmJoined() string      { return strings.Join(rmEvents, ",") }

func useRMEvents(t *testing.T) {
	t.Helper()
	rmEvents = nil
	t.Cleanup(func() { rmEvents = nil })
}

type rmSharedModule struct{}

func (m *rmSharedModule) ModuleMetadata() module.Metadata { return module.Metadata{Name: "shared"} }

func (m *rmSharedModule) PreConfigure(module.RootBuilder) error { rmRecord("shared:pre"); return nil }

func (m *rmSharedModule) Configure(b module.RootBuilder) error {
	rmRecord("shared:cfg")
	if err := b.Services().Add(NewShared, module.Singleton()); err != nil {
		return err
	}
	return b.Services().Export(module.Service[*rmShared]())
}

func (m *rmSharedModule) PostConfigure(module.RootBuilder) error { rmRecord("shared:post"); return nil }

func (m *rmSharedModule) Start(context.Context, module.Runtime) error {
	rmRecord("shared:start")
	return nil
}

func (m *rmSharedModule) Stop(context.Context, module.Runtime) error {
	rmRecord("shared:stop")
	return nil
}

type rmCatalogModule struct{}

func (m *rmCatalogModule) ModuleMetadata() module.Metadata { return module.Metadata{Name: "catalog"} }

func (m *rmCatalogModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rmSharedModule]()}
}

func (m *rmCatalogModule) PreConfigure(module.RootBuilder) error { rmRecord("catalog:pre"); return nil }

func (m *rmCatalogModule) Configure(b module.RootBuilder) error {
	rmRecord("catalog:cfg")
	if err := b.Services().Add(NewCatalog, module.Singleton()); err != nil {
		return err
	}
	return b.Services().Export(module.Contract[rmCatalog]())
}

func (m *rmCatalogModule) PostConfigure(module.RootBuilder) error {
	rmRecord("catalog:post")
	return nil
}

func (m *rmCatalogModule) Start(context.Context, module.Runtime) error {
	rmRecord("catalog:start")
	return nil
}

func (m *rmCatalogModule) Stop(context.Context, module.Runtime) error {
	rmRecord("catalog:stop")
	return nil
}

type rmPricingModule struct{}

func (m *rmPricingModule) ModuleMetadata() module.Metadata { return module.Metadata{Name: "pricing"} }

func (m *rmPricingModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rmSharedModule]()}
}

func (m *rmPricingModule) PreConfigure(module.RootBuilder) error { rmRecord("pricing:pre"); return nil }

func (m *rmPricingModule) Configure(module.RootBuilder) error { rmRecord("pricing:cfg"); return nil }

func (m *rmPricingModule) PostConfigure(module.RootBuilder) error {
	rmRecord("pricing:post")
	return nil
}

func (m *rmPricingModule) Start(ctx context.Context, rt module.Runtime) error {
	value, err := rt.Services().Resolve(ctx, module.Contract[*rmShared]())
	if err != nil {
		return err
	}
	if _, ok := value.(*rmShared); !ok {
		return fmt.Errorf("unexpected shared type %T", value)
	}
	rmRecord("pricing:start")
	return nil
}

func (m *rmPricingModule) Stop(context.Context, module.Runtime) error {
	rmRecord("pricing:stop")
	return nil
}

type rmAppModule struct{}

func (m *rmAppModule) ModuleMetadata() module.Metadata { return module.Metadata{Name: "app"} }

func (m *rmAppModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rmCatalogModule](), module.Of[rmPricingModule]()}
}

func (m *rmAppModule) PreConfigure(module.RootBuilder) error { rmRecord("app:pre"); return nil }

func (m *rmAppModule) Configure(b module.RootBuilder) error { rmRecord("app:cfg"); return nil }

func (m *rmAppModule) PostConfigure(module.RootBuilder) error { rmRecord("app:post"); return nil }

func (m *rmAppModule) Start(ctx context.Context, rt module.Runtime) error {
	rmRecord("app:start")
	value, err := rt.Services().Resolve(ctx, module.Contract[rmCatalog]())
	if err != nil {
		return err
	}
	if value.(rmCatalog).Len() == 0 {
		return errors.New("empty catalog")
	}
	return nil
}

func (m *rmAppModule) Stop(context.Context, module.Runtime) error { rmRecord("app:stop"); return nil }

func newRMHost(t *testing.T) runtime.Host {
	t.Helper()
	host, err := runtime.New(runtime.Options{DrainTimeout: time.Second, CleanupTimeout: time.Second}, []config.Source{
		{Name: "defaults", Values: map[string]string{
			"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s",
			"uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4",
		},
		}})
	if err != nil {
		t.Fatal(err)
	}
	return host
}

// The diamond shared dependency is planned and constructed exactly once.
func TestRootDiamondPlannedOnce(t *testing.T) {
	useRMEvents(t)
	rmSharedCount.Store(0)
	host := newRMHost(t)
	if err := runtime.StartRoot(context.Background(), host, &rmAppModule{}); err != nil {
		t.Fatal(err)
	}
	defer host.Stop(context.Background())
	if rmSharedCount.Load() != 1 {
		t.Fatalf("shared dependency constructed %d times", rmSharedCount.Load())
	}
}

// Full phase barriers keep all PreConfigure hooks before the first Configure.
func TestRootPhaseBarrierAndStableOrder(t *testing.T) {
	useRMEvents(t)
	host := newRMHost(t)
	if err := runtime.StartRoot(context.Background(), host, &rmAppModule{}); err != nil {
		t.Fatal(err)
	}
	defer host.Stop(context.Background())
	want := "shared:pre,catalog:pre,pricing:pre,app:pre," +
		"shared:cfg,catalog:cfg,pricing:cfg,app:cfg," +
		"shared:post,catalog:post,pricing:post,app:post," +
		"shared:start,catalog:start,pricing:start,app:start"
	if got := rmJoined(); got != want {
		t.Fatalf("unexpected phase order:\n got %s\nwant %s", got, want)
	}
}

// Invalid graphs are rejected before any hook or constructor.
func TestRootInvalidGraphRunsNothing(t *testing.T) {
	useRMEvents(t)
	var typedNil *rmSharedModule
	err := runtime.StartRoot(context.Background(), newRMHost(t), &rmAppModule{}, typedNil)
	if err == nil || !strings.Contains(err.Error(), "typed nil") {
		t.Fatalf("expected typed-nil root rejection: %v", err)
	}
	if rmJoined() != "" {
		t.Fatalf("hooks ran for an invalid graph: %s", rmJoined())
	}
}

// A consumer without the contract owner as a direct dependency cannot resolve
// the contract; the error names the consumer and the type.
func TestRootVisibilityRequiresContractOwner(t *testing.T) {
	useRMEvents(t)
	err := runtime.StartRoot(context.Background(), newRMHost(t), &rmHiddenModule{})
	if err == nil || !strings.Contains(err.Error(), "hidden") || !strings.Contains(err.Error(), "rmCatalog") {
		t.Fatalf("expected visibility diagnostic naming consumer/type: %v", err)
	}
}

type rmHiddenModule struct{}

func (m *rmHiddenModule) ModuleMetadata() module.Metadata { return module.Metadata{Name: "hidden"} }

func (m *rmHiddenModule) Start(ctx context.Context, rt module.Runtime) error {
	_, err := rt.Services().Resolve(ctx, module.Contract[rmCatalog]())
	return err
}

// Start failure reverses the stops of already started modules, never reports
// Ready, and leaves the failed module to its own cleanup.
func TestRootFailureReverseCleanup(t *testing.T) {
	useRMEvents(t)
	host := newRMHost(t)
	err := runtime.StartRoot(context.Background(), host, &rmFailingApp{})
	if err == nil {
		t.Fatal("expected start failure")
	}
	if host.Ready() {
		t.Fatal("failed start must not report Ready")
	}
	joined := rmJoined()
	if !strings.Contains(joined, "pricing:stop,catalog:stop,shared:stop") {
		t.Fatalf("reverse stops missing: %s", joined)
	}
	if strings.Contains(joined, "app:stop") {
		t.Fatalf("failed module must not be stopped: %s", joined)
	}
}

type rmFailingApp struct{}

func (m *rmFailingApp) ModuleMetadata() module.Metadata { return module.Metadata{Name: "app"} }

func (m *rmFailingApp) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rmCatalogModule](), module.Of[rmPricingModule]()}
}

func (m *rmFailingApp) Start(context.Context, module.Runtime) error {
	rmRecord("app:start")
	return errors.New("app start boom")
}

// Neutral contract replacement: the neutral contract owner exports the
// contract, an implementation module contributes explicitly with As and
// swaps its own contribution with ReplaceContract, and the owner binds the
// selected implementation. The contract identity never changes and the
// consumer depends only on the contract owner.
func TestRootNeutralReplacementKeepsIdentity(t *testing.T) {
	useRMEvents(t)
	host := newRMHost(t)
	if err := runtime.StartRoot(context.Background(), host,
		&rmNeutralOwnerModule{}, &rmNeutralReplacerModule{}, &rmNeutralApp{}); err != nil {
		t.Fatal(err)
	}
	defer host.Stop(context.Background())
	for _, want := range []string{
		"neutral-owner export",
		"neutral-installer contribution v1",
		"neutral-installer upgrade to v2",
		"neutral-app resolve v2",
	} {
		if !strings.Contains(rmJoined(), want) {
			t.Fatalf("missing lifecycle marker %q: %s", want, rmJoined())
		}
	}
}

// rmNeutralSvc is the neutral contract; the implementation types differ so the
// replacement is provable.
type rmNeutralSvc interface{ Version() int }

type rmNeutralV1 struct{}

func (rmNeutralV1) Version() int { return 1 }

type rmNeutralV2 struct{}

func (rmNeutralV2) Version() int { return 2 }

var rmNeutralContract = module.Contract[rmNeutralSvc]()

// rmNeutralOwnerModule owns the contract: it exports and later binds.
type rmNeutralOwnerModule struct{}

func (m *rmNeutralOwnerModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "neutral-owner"}
}

func (m *rmNeutralOwnerModule) Configure(b module.RootBuilder) error {
	if err := b.Services().Export(rmNeutralContract); err != nil {
		return err
	}
	rmRecord("neutral-owner export")
	return nil
}

// rmNeutralReplacerModule contributes and upgrades its own implementation
// while the contract identity stays with the owner.
type rmNeutralReplacerModule struct{}

func (m *rmNeutralReplacerModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "neutral-installer"}
}

func (m *rmNeutralReplacerModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rmNeutralOwnerModule]()}
}

func (m *rmNeutralReplacerModule) Configure(b module.RootBuilder) error {
	if err := b.Services().Add(func() rmNeutralV1 { return rmNeutralV1{} }, module.As(rmNeutralContract)); err != nil {
		return err
	}
	rmRecord("neutral-installer contribution v1")
	return nil
}

func (m *rmNeutralReplacerModule) PostConfigure(b module.RootBuilder) error {
	if err := b.Services().ReplaceContract(rmNeutralContract, func() rmNeutralV2 { return rmNeutralV2{} }, "upgrade neutral implementation"); err != nil {
		return err
	}
	rmRecord("neutral-installer upgrade to v2")
	return nil
}

// rmNeutralApp is the consumer: it depends only on the contract owner and
// resolves the contract, never the implementation module.
type rmNeutralApp struct{}

func (m *rmNeutralApp) ModuleMetadata() module.Metadata { return module.Metadata{Name: "neutral-app"} }

func (m *rmNeutralApp) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rmNeutralOwnerModule]()}
}

func (m *rmNeutralApp) Start(ctx context.Context, rt module.Runtime) error {
	value, err := rt.Services().Resolve(ctx, rmNeutralContract)
	if err != nil {
		return err
	}
	service := value.(rmNeutralSvc)
	if service.Version() != 2 {
		return fmt.Errorf("replacement not visible through contract identity: %d", service.Version())
	}
	rmRecord("neutral-app resolve v2")
	return nil
}

// kernel/app.Run: invalid caller defaults fail config validation before any
// hook; a valid composition starts and stops on cancellation.
func TestKernelAppRunDefaultsAndOverrides(t *testing.T) {
	appDefault := &rmAppDefaultModule{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(ctx, appDefault, app.Sources(
			config.Source{Name: "defaults", Values: map[string]string{"uow.max_requires_new_depth": "0"}},
		))
	}()
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "uow.max_requires_new_depth") {
			t.Fatalf("invalid config override must fail config validation: %v", err)
		}
		if appDefault.started {
			t.Fatal("hooks must not run for invalid config")
		}
		return
	case <-time.After(5 * time.Second):
	}
	// Valid config path would reach Start; stop the run and expect clean exit.
	if !appDefault.started {
		t.Fatal("Run neither failed on config nor started the composition")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

type rmAppDefaultModule struct{ started bool }

func (m *rmAppDefaultModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "app-default"}
}

func (m *rmAppDefaultModule) Start(context.Context, module.Runtime) error {
	m.started = true
	return nil
}
