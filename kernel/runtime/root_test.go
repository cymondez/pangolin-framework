package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/kernel/di"
	"github.com/cymondez/pangolin-framework/kernel/module"
)

type rootStoreService struct{ items map[string]string }

func (s *rootStoreService) Put(id, title string) error {
	s.items[id] = title
	return nil
}
func (s *rootStoreService) Get(id string) (string, bool) {
	title, ok := s.items[id]
	return title, ok
}

func NewRootStore() *rootStoreService { return &rootStoreService{items: map[string]string{}} }

type RootCatalog interface {
	Put(id, title string) error
	Get(id string) (string, bool)
	Len() int
}

type rootCatalogService struct{ store *rootStoreService }

func (s *rootCatalogService) Put(id, title string) error { return s.store.Put(id, title) }
func (s *rootCatalogService) Get(id string) (string, bool) {
	return s.store.Get(id)
}
func (s *rootCatalogService) Len() int { return len(s.store.items) }

func NewRootCatalog(store *rootStoreService) *rootCatalogService {
	return &rootCatalogService{store: store}
}

// rootEvents records lifecycle events for the sequential root fixtures. The
// loader zero-instantiates dependency Types, so the shared recorder lives at
// package level and every hook checks for it; tests run sequentially here.
var rootEvents []string

func rootEvent(event string) { rootEvents = append(rootEvents, event) }
func rootJoined() string     { return strings.Join(rootEvents, ",") }

func useRootEvents(t *testing.T) {
	t.Helper()
	rootEvents = nil
	t.Cleanup(func() { rootEvents = nil })
}

// rootStoreModule carries a probe field: empty-struct pointers alias in Go,
// which would make two distinct root instances compare equal in the loader.
type rootStoreModule struct{ probe bool }

func (m *rootStoreModule) ModuleMetadata() module.Metadata { return module.Metadata{Name: "store"} }

func (m *rootStoreModule) PreConfigure(module.RootBuilder) error {
	rootEvent("store:pre")
	return nil
}

func (m *rootStoreModule) Configure(b module.RootBuilder) error {
	rootEvent("store:cfg")
	if err := b.Services().Add(func() *rootStoreService {
		return NewRootStore()
	}, module.Singleton()); err != nil {
		return err
	}
	// The exact concrete service is a public contract owned by this module.
	return b.Services().Export(module.Service[*rootStoreService]())
}

func (m *rootStoreModule) PostConfigure(module.RootBuilder) error {
	rootEvent("store:post")
	return nil
}

func (m *rootStoreModule) Start(context.Context, module.Runtime) error {
	rootEvent("store:start")
	return nil
}

func (m *rootStoreModule) Stop(context.Context, module.Runtime) error {
	rootEvent("store:stop")
	return nil
}

type rootCatalogModule struct{}

func (m *rootCatalogModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "catalog"}
}

func (m *rootCatalogModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rootStoreModule]()}
}

func (m *rootCatalogModule) PreConfigure(module.RootBuilder) error {
	rootEvent("catalog:pre")
	return nil
}

func (m *rootCatalogModule) Configure(b module.RootBuilder) error {
	rootEvent("catalog:cfg")
	if err := b.Services().Add(NewRootCatalog, module.Singleton()); err != nil {
		return err
	}
	return b.Services().Export(module.Contract[RootCatalog]())
}

func (m *rootCatalogModule) PostConfigure(module.RootBuilder) error {
	rootEvent("catalog:post")
	return nil
}

func (m *rootCatalogModule) Start(context.Context, module.Runtime) error {
	rootEvent("catalog:start")
	return nil
}

func (m *rootCatalogModule) Stop(context.Context, module.Runtime) error {
	rootEvent("catalog:stop")
	return nil
}

type rootAppModule struct {
	saw       string
	startHook func(context.Context, module.Runtime) error
}

func (m *rootAppModule) ModuleMetadata() module.Metadata { return module.Metadata{Name: "app"} }

func (m *rootAppModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rootCatalogModule]()}
}

func (m *rootAppModule) PreConfigure(module.RootBuilder) error { rootEvent("app:pre"); return nil }

func (m *rootAppModule) Configure(b module.RootBuilder) error {
	rootEvent("app:cfg")
	return b.SetDefault("app.name", "root")
}

func (m *rootAppModule) PostConfigure(module.RootBuilder) error { rootEvent("app:post"); return nil }

func (m *rootAppModule) Start(ctx context.Context, rt module.Runtime) error {
	rootEvent("app:start")
	if name, ok := rt.Config().Lookup("app.name"); !ok || name != "root" {
		return fmt.Errorf("app default config missing: %q %v", name, ok)
	}
	if m.startHook != nil {
		return m.startHook(ctx, rt)
	}
	value, err := rt.Services().Resolve(ctx, module.Contract[RootCatalog]())
	if err != nil {
		return err
	}
	catalog := value.(RootCatalog)
	if err := catalog.Put("demo", "root-composed"); err != nil {
		return err
	}
	m.saw, _ = catalog.Get("demo")
	return nil
}

func (m *rootAppModule) Stop(context.Context, module.Runtime) error {
	rootEvent("app:stop")
	return nil
}

type rootHiddenModule struct{}

func (m *rootHiddenModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "hidden"}
}

// No DependsOn: resolving the catalog contract must be rejected.
func (m *rootHiddenModule) Start(ctx context.Context, rt module.Runtime) error {
	_, err := rt.Services().Resolve(ctx, module.Contract[RootCatalog]())
	return err
}

type rootSaveThenFailModule struct{ saved *module.RootBuilder }

func (m *rootSaveThenFailModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "savefail"}
}

func (m *rootSaveThenFailModule) Configure(b module.RootBuilder) error {
	rootEvent("savefail:cfg")
	b2 := b
	*m.saved = b2
	return fmt.Errorf("savefail boom")
}

func newRootTestHost(t *testing.T) Host {
	t.Helper()
	host, err := New(Options{DrainTimeout: time.Second, CleanupTimeout: time.Second}, []config.Source{
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

func TestRootStartFullLifecycle(t *testing.T) {
	useRootEvents(t)
	app := &rootAppModule{}
	host := newRootTestHost(t)
	if err := StartRoot(context.Background(), host, app); err != nil {
		t.Fatal(err)
	}
	if !host.Ready() {
		t.Fatal("host not ready after successful root start")
	}
	if app.saw != "root-composed" {
		t.Fatalf("root did not resolve catalog contract: %q", app.saw)
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := host.Stop(cleanup); err != nil {
		t.Fatal(err)
	}
	want := "store:pre,catalog:pre,app:pre,store:cfg,catalog:cfg,app:cfg,store:post,catalog:post,app:post,store:start,catalog:start,app:start,app:stop,catalog:stop,store:stop"
	if got := rootJoined(); got != want {
		t.Fatalf("unexpected lifecycle events:\n got %s\nwant %s", got, want)
	}
}

func TestRootPhaseBarriersOrder(t *testing.T) {
	useRootEvents(t)
	app := &rootAppModule{startHook: func(context.Context, module.Runtime) error { return nil }}
	host := newRootTestHost(t)
	if err := StartRoot(context.Background(), host, app); err != nil {
		t.Fatal(err)
	}
	wantPrefix := "store:pre,catalog:pre,app:pre,store:cfg,catalog:cfg,app:cfg,store:post,catalog:post,app:post"
	if got := rootJoined(); !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("phase barrier violated:\n got %s\nwant prefix %s", got, wantPrefix)
	}
}

// A consumer without a direct dependency on the contract owner cannot resolve
// the contract through its restricted resolver; the diagnostic names both.
func TestRootVisibilityBoundary(t *testing.T) {
	useRootEvents(t)
	hidden := &rootHiddenModule{}
	host := newRootTestHost(t)
	err := StartRoot(context.Background(), host, hidden)
	if err == nil {
		t.Fatal("expected visibility error")
	}
	if !strings.Contains(err.Error(), "hidden") || !strings.Contains(err.Error(), "RootCatalog") {
		t.Fatalf("diagnostic must name consumer/type: %v", err)
	}
}

func TestRootReverseStopOrderOnFailure(t *testing.T) {
	useRootEvents(t)
	app := &rootAppModule{startHook: func(context.Context, module.Runtime) error {
		return fmt.Errorf("app start boom")
	}}
	host := newRootTestHost(t)
	if err := StartRoot(context.Background(), host, app); err == nil {
		t.Fatal("expected start failure")
	}
	if host.Ready() {
		t.Fatal("failed start must not report Ready")
	}
	got := rootJoined()
	if !strings.Contains(got, "catalog:stop") || !strings.Contains(got, "store:stop") {
		t.Fatalf("reverse stops missing: %s", got)
	}
	if strings.Contains(got, "app:stop") {
		t.Fatalf("failed module must not be stopped by the host: %s", got)
	}
}

func TestRootInvalidGraphRunsNoHooks(t *testing.T) {
	useRootEvents(t)
	// Two distinct instances of the same root type are rejected before any
	// hook or constructor.
	err := StartRoot(context.Background(), newRootTestHost(t), &rootStoreModule{}, &rootStoreModule{})
	if err == nil || !strings.Contains(err.Error(), "distinct instances") {
		t.Fatalf("expected distinct-instance rejection: %v", err)
	}
	if got := rootJoined(); got != "" {
		t.Fatalf("hooks ran for invalid graph: %s", got)
	}
}

// A hook error must not leave a usable builder behind: the saved registrar
// rejects every later operation.
func TestRootBuilderInvalidatedAfterHookError(t *testing.T) {
	useRootEvents(t)
	saved := module.RootBuilder(nil)
	boom := &rootSaveThenFailModule{saved: &saved}
	host := newRootTestHost(t)
	if err := StartRoot(context.Background(), host, boom); err == nil {
		t.Fatal("expected configure failure")
	}
	if saved == nil {
		t.Fatal("fixture did not capture the builder")
	}
	if err := saved.Services().Add(NewRootStore); err == nil {
		t.Fatal("saved registrar must be rejected after Revoke")
	}
	if err := saved.Services().Export(module.Service[*rootStoreService]()); err == nil {
		t.Fatal("saved registrar Export must be rejected after Revoke")
	}
	// Every remaining registrar operation must report the revoked window.
	if err := saved.Replace(di.Provider{Key: di.NewKey[int]("revoked.probe"), Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		return 1, nil, nil
	}}, "reason"); err == nil {
		t.Fatal("saved registrar Replace must be rejected after Revoke")
	}
	if err := saved.Services().Bind(module.Service[*rootStoreService](), NewRootStore); err == nil {
		t.Fatal("saved registrar Bind must be rejected after Revoke")
	}
	if err := saved.Services().ReplaceBinding(module.Service[*rootStoreService](), NewRootStore, "reason"); err == nil {
		t.Fatal("saved registrar ReplaceBinding must be rejected after Revoke")
	}
	if err := saved.Services().ReplaceContract(module.Contract[RootCatalog](), NewRootCatalog, "reason"); err == nil {
		t.Fatal("saved registrar ReplaceContract must be rejected after Revoke")
	}
}

// A panicking hook still revokes its window and the host reclaims resources;
// no start happens and the failure propagates exactly like the legacy path.
func TestRootHookPanicReclaimsResources(t *testing.T) {
	useRootEvents(t)
	host := newRootTestHost(t)
	defer func() {
		if p := recover(); p == nil {
			t.Fatal("expected hook panic to propagate after cleanup")
		}
		if strings.Contains(rootJoined(), "start") {
			t.Fatalf("panic before Start must not produce starts: %s", rootJoined())
		}
	}()
	_ = StartRoot(context.Background(), host, &rootPanickyConfigureModule{})
	rootEvent("unreachable")
}

type rootPanickyConfigureModule struct{}

func (m *rootPanickyConfigureModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "panicky"}
}

func (m *rootPanickyConfigureModule) Configure(module.RootBuilder) error {
	panic("hook boom")
}

// rootPanicSentinel must survive the hook panic as the identical pointer; the
// legacy path rethrows the original value and root mode must match it.
type rootPanicSentinel struct{ marker string }

// rootPanicParticipant records the drain participants of the host shutdown:
// the hook-panic failure path must reclaim host resources like the legacy path.
type rootPanicParticipant struct {
	quiesced, drained atomic.Bool
}

func (p *rootPanicParticipant) Quiesce(context.Context) error { p.quiesced.Store(true); return nil }
func (p *rootPanicParticipant) Drain(context.Context) error   { p.drained.Store(true); return nil }
func (p *rootPanicParticipant) Cancel()                       {}

// rootPanickySaveModule registers a service, saves its builder and panics with
// the exact sentinel pointer.
type rootPanickySaveModule struct {
	saved    *module.RootBuilder
	sentinel *rootPanicSentinel
}

func (m *rootPanickySaveModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "panicky-save"}
}

func (m *rootPanickySaveModule) Configure(b module.RootBuilder) error {
	if err := b.Services().Add(NewRootStore, module.Singleton()); err != nil {
		return err
	}
	*m.saved = b
	panic(m.sentinel)
}

// A hook panic must surface the original panic value unmodified (no phase
// stringification), reclaim host resources through the normal shutdown path
// and revoke both the builder window and the saved engine registrar.
func TestRootHookPanicPreservesOriginalValue(t *testing.T) {
	useRootEvents(t)
	sentinel := &rootPanicSentinel{marker: "boom"}
	saved := module.RootBuilder(nil)
	probe := &rootPanickySaveModule{saved: &saved, sentinel: sentinel}
	participant := &rootPanicParticipant{}
	host, err := New(Options{DrainTimeout: time.Second, CleanupTimeout: time.Second}, []config.Source{
		{Name: "defaults", Values: map[string]string{
			"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s",
			"uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4",
		}},
	}, participant)
	if err != nil {
		t.Fatal(err)
	}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = StartRoot(context.Background(), host, probe)
	}()
	if recovered == nil {
		t.Fatal("expected hook panic to propagate after cleanup")
	}
	if _, ok := recovered.(*rootPanicSentinel); !ok {
		t.Fatalf("hook panic must keep the original value and type, got %T", recovered)
	}
	if recovered != sentinel {
		t.Fatal("recovered panic must be the identical sentinel pointer")
	}
	if !participant.quiesced.Load() || !participant.drained.Load() {
		t.Fatal("shutdown participants must run on the hook-panic failure path")
	}
	if host.Ready() {
		t.Fatal("failed start must not report Ready")
	}
	if got := rootJoined(); got != "" {
		t.Fatalf("panic before Start must run no lifecycle hooks: %s", got)
	}
	if err = StartRoot(context.Background(), host, probe); err == nil || !strings.Contains(err.Error(), "cannot start") {
		t.Fatalf("host must be consumed after the panic-path shutdown: %v", err)
	}
	if err = host.Stop(context.Background()); err != nil {
		t.Fatalf("Stop must stay idempotent after the panic-path shutdown: %v", err)
	}
	if err = saved.Services().Add(NewRootStore); err == nil {
		t.Fatal("saved registrar must be rejected after the hook panic")
	}
	if err = saved.Services().Export(module.Service[*rootStoreService]()); err == nil {
		t.Fatal("saved registrar Export must be rejected after the hook panic")
	}
	if err = saved.SetDefault("panicprobe.key", "v"); err == nil {
		t.Fatal("saved builder SetDefault must be rejected after the hook panic")
	}
}

// Deterministic revoke semantics: Revoke waits for an in-flight operation to
// finish (chan-signaled, no sleeps) and rejects operations started afterwards.
func TestRootBuilderRevokeWaitsInFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	fake := &rootFakeComposition{onProvide: func() {
		close(started)
		<-release
	}}
	defaults := map[string]string{}
	b := newRootBuilder(fake, "conc", defaults)

	// The live window validates keys before any in-flight accounting.
	if err := b.SetDefault("", "v"); err == nil || !strings.Contains(err.Error(), "empty default key") {
		t.Fatalf("empty default key must be rejected: %v", err)
	}
	if err := b.SetDefault("dup", "first"); err != nil {
		t.Fatalf("first default must be recorded: %v", err)
	}
	if err := b.SetDefault("dup", "second"); err == nil || !strings.Contains(err.Error(), "duplicate default dup") {
		t.Fatalf("duplicate default must be rejected: %v", err)
	}

	opDone := make(chan error, 1)
	go func() { opDone <- b.Provide(di.Provider{}) }()
	<-started
	revoked := make(chan struct{})
	go func() { b.revoke(); close(revoked) }()
	// Revoke must not complete while the Provide is still running.
	select {
	case <-revoked:
		t.Fatal("revoke completed while an operation was in flight")
	default:
	}
	close(release)
	if err := <-opDone; err != nil {
		t.Fatalf("in-flight operation must complete normally: %v", err)
	}
	<-revoked
	if err := b.Provide(di.Provider{}); err == nil {
		t.Fatal("post-revoke operation must be rejected")
	}
	if err := b.SetDefault("k", "v"); err == nil {
		t.Fatal("post-revoke SetDefault must be rejected")
	}
	// The revoked composition seam hands out a registrar that reports the
	// revoked window for every operation instead of nil-panicking the hook.
	svc := b.Services()
	if err := svc.Add(NewRootStore); err == nil {
		t.Fatal("revoked registrar Add must be rejected")
	}
	if err := svc.Replace(NewRootStore, "reason"); err == nil {
		t.Fatal("revoked registrar Replace must be rejected")
	}
	if err := svc.Export(module.Service[*rootStoreService]()); err == nil {
		t.Fatal("revoked registrar Export must be rejected")
	}
	if err := svc.Bind(module.Service[*rootStoreService](), NewRootStore); err == nil {
		t.Fatal("revoked registrar Bind must be rejected")
	}
	if err := svc.ReplaceBinding(module.Service[*rootStoreService](), NewRootStore, "reason"); err == nil {
		t.Fatal("revoked registrar ReplaceBinding must be rejected")
	}
	if err := svc.ReplaceContract(module.Contract[RootCatalog](), NewRootCatalog, "reason"); err == nil {
		t.Fatal("revoked registrar ReplaceContract must be rejected")
	}
}

// rootFakeComposition drives rootBuilder without the module engine.
type rootFakeComposition struct {
	onProvide func()
}

func (f *rootFakeComposition) services(owner string) module.Services {
	return erroringServices{owner: owner}
}
func (f *rootFakeComposition) provide(owner string, p di.Provider) error {
	f.onProvide()
	return nil
}
func (f *rootFakeComposition) replace(owner string, p di.Provider, reason string) error {
	return nil
}

// The legacy path keeps running unchanged alongside the root path.
func TestLegacyHostStartUnchanged(t *testing.T) {
	started := false
	stopped := false
	host, err := New(Options{DrainTimeout: time.Second, CleanupTimeout: time.Second}, []config.Source{
		{Name: "defaults", Values: map[string]string{
			"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s",
			"uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	legacy := module.Module{
		Name: "legacy", Version: "0.1.0",
		Start: func(context.Context, module.Runtime) error { started = true; return nil },
		Stop:  func(context.Context, module.Runtime) error { stopped = true; return nil },
	}
	if err := host.Start(context.Background(), []module.Module{legacy}); err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !started || !stopped {
		t.Fatalf("legacy path broken: %v %v", started, stopped)
	}
}

// ---- Stop resolver visibility: A -> B -> C chains on the host lifecycle.

var rootStopCleanupHits atomic.Int32

type rootStopBaseService struct{}

func (s *rootStopBaseService) Ping() string { return "base" }

type rootStopBaseModule struct{}

func (m *rootStopBaseModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "stop-base"}
}

func (m *rootStopBaseModule) Configure(b module.RootBuilder) error {
	if err := b.Provide(di.Provider{Key: di.NewKey[int]("stop.cleanup-probe"), Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		return 1, func(context.Context) error { rootStopCleanupHits.Add(1); return nil }, nil
	}}); err != nil {
		return err
	}
	if err := b.Services().Add(func() *rootStopBaseService { return &rootStopBaseService{} }, module.Singleton()); err != nil {
		return err
	}
	return b.Services().Export(module.Service[*rootStopBaseService]())
}

func (m *rootStopBaseModule) Stop(context.Context, module.Runtime) error {
	rootEvent("base:stop")
	return nil
}

type rootStopMidService struct{ base *rootStopBaseService }

func (s *rootStopMidService) Ping() string { return "mid:" + s.base.Ping() }

type RootStopPublic interface{ Ping() string }

type rootStopMidModule struct{}

func (m *rootStopMidModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "stop-mid"}
}

func (m *rootStopMidModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rootStopBaseModule]()}
}

func (m *rootStopMidModule) Configure(b module.RootBuilder) error {
	if err := b.Services().Add(func(base *rootStopBaseService) *rootStopMidService {
		return &rootStopMidService{base: base}
	}, module.Singleton()); err != nil {
		return err
	}
	return b.Services().Export(module.Contract[RootStopPublic]())
}

func (m *rootStopMidModule) Stop(context.Context, module.Runtime) error {
	rootEvent("mid:stop")
	return nil
}

// rootStopTopModule consumes the public mid contract but declares no edge to
// stop-base; its Stop must not be able to resolve base services silently.
type rootStopTopModule struct{}

func (m *rootStopTopModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "stop-top"}
}

func (m *rootStopTopModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rootStopMidModule]()}
}

func (m *rootStopTopModule) Start(ctx context.Context, rt module.Runtime) error {
	rootEvent("top:start")
	value, err := rt.Services().Resolve(ctx, module.Contract[RootStopPublic]())
	if err != nil {
		return err
	}
	if value.(RootStopPublic).Ping() != "mid:base" {
		return fmt.Errorf("unexpected public pipeline: %v", value)
	}
	return nil
}

func (m *rootStopTopModule) Stop(_ context.Context, rt module.Runtime) error {
	rootEvent("top:stop")
	_, err := rt.Services().Resolve(context.Background(), module.Service[*rootStopBaseService]())
	return err
}

// rootStopTopDirectModule additionally declares the direct Type edge to the
// service owner, so the same Stop resolution is legal.
type rootStopTopDirectModule struct{}

func (m *rootStopTopDirectModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "stop-top"}
}

func (m *rootStopTopDirectModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[rootStopMidModule](), module.Of[rootStopBaseModule]()}
}

func (m *rootStopTopDirectModule) Start(ctx context.Context, rt module.Runtime) error {
	rootEvent("top:start")
	if _, err := rt.Services().Resolve(ctx, module.Contract[RootStopPublic]()); err != nil {
		return err
	}
	return nil
}

func (m *rootStopTopDirectModule) Stop(_ context.Context, rt module.Runtime) error {
	rootEvent("top:stop")
	value, err := rt.Services().Resolve(context.Background(), module.Service[*rootStopBaseService]())
	if err != nil {
		return err
	}
	if value.(*rootStopBaseService).Ping() != "base" {
		return fmt.Errorf("unexpected base service: %v", value)
	}
	return nil
}

func TestRootStopResolverVisibilityRejectsHiddenDependencies(t *testing.T) {
	useRootEvents(t)
	rootStopCleanupHits.Store(0)
	t.Cleanup(func() { rootStopCleanupHits.Store(0) })
	host := newRootTestHost(t)
	if err := StartRoot(context.Background(), host, &rootStopTopModule{}); err != nil {
		t.Fatal(err)
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stopErr := host.Stop(cleanup)
	if stopErr == nil {
		t.Fatal("expected Stop to surface the hidden-dependency error")
	}
	for _, fragment := range []string{"stop-top cannot resolve", "owned by module stop-base", "declare a direct dependency on stop-base"} {
		if !strings.Contains(stopErr.Error(), fragment) {
			t.Fatalf("missing diagnostic %q in: %v", fragment, stopErr)
		}
	}
	for _, event := range []string{"top:stop", "mid:stop", "base:stop"} {
		if got := strings.Count(rootJoined(), event); got != 1 {
			t.Fatalf("stop event %s ran %d times: %s", event, got, rootJoined())
		}
	}
	if !strings.HasSuffix(rootJoined(), "top:stop,mid:stop,base:stop") {
		t.Fatalf("stops must run in reverse order: %s", rootJoined())
	}
	if got := rootStopCleanupHits.Load(); got != 1 {
		t.Fatalf("container cleanup ran %d times despite the Stop error", got)
	}
}

func TestRootStopResolverVisibilityAllowsDirectEdge(t *testing.T) {
	useRootEvents(t)
	rootStopCleanupHits.Store(0)
	t.Cleanup(func() { rootStopCleanupHits.Store(0) })
	host := newRootTestHost(t)
	if err := StartRoot(context.Background(), host, &rootStopTopDirectModule{}); err != nil {
		t.Fatal(err)
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if stopErr := host.Stop(cleanup); stopErr != nil {
		t.Fatal(stopErr)
	}
	if !strings.HasSuffix(rootJoined(), "top:stop,mid:stop,base:stop") {
		t.Fatalf("stops must run in reverse order: %s", rootJoined())
	}
	if got := rootStopCleanupHits.Load(); got != 1 {
		t.Fatalf("container cleanup ran %d times", got)
	}
}

// ---- Parallel SetDefault on one builder window.

type rootParallelDefaultsModule struct {
	shared atomic.Int32
}

func (m *rootParallelDefaultsModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "parallel"}
}

// Configure performs a barrier-synchronized race on the contended default and
// records the unique per-worker defaults; the window mutex must turn the race
// into exactly one winner plus duplicate rejections.
func (m *rootParallelDefaultsModule) Configure(b module.RootBuilder) error {
	const workers = 4
	ready := make(chan struct{})
	errs := make(chan error, workers)
	var arrived, done sync.WaitGroup
	arrived.Add(workers)
	done.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer done.Done()
			arrived.Done()
			<-ready
			if err := b.SetDefault(fmt.Sprintf("parallel.worker%d", i), "true"); err != nil {
				errs <- err
				return
			}
			if err := b.SetDefault("parallel.shared", fmt.Sprintf("v%d", i)); err != nil {
				errs <- err
				return
			}
			m.shared.Add(1)
		}()
	}
	arrived.Wait()
	close(ready)
	done.Wait()
	close(errs)
	for err := range errs {
		if !strings.Contains(err.Error(), "duplicate default parallel.shared") {
			return err
		}
	}
	if m.shared.Load() != 1 {
		return fmt.Errorf("expected exactly one SetDefault winner, got %d", m.shared.Load())
	}
	return nil
}

func (m *rootParallelDefaultsModule) Start(_ context.Context, rt module.Runtime) error {
	rootEvent("parallel:start")
	for i := 0; i < 4; i++ {
		if _, ok := rt.Config().Lookup(fmt.Sprintf("parallel.worker%d", i)); !ok {
			return fmt.Errorf("parallel default missing: worker%d", i)
		}
	}
	if _, ok := rt.Config().Lookup("parallel.shared"); !ok {
		return fmt.Errorf("parallel default missing: shared")
	}
	return nil
}

func TestRootBuilderParallelSetDefaultRace(t *testing.T) {
	useRootEvents(t)
	parallel := &rootParallelDefaultsModule{}
	host := newRootTestHost(t)
	if err := StartRoot(context.Background(), host, parallel); err != nil {
		t.Fatal(err)
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := host.Stop(cleanup); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rootJoined(), "parallel:start") {
		t.Fatalf("parallel module did not start: %s", rootJoined())
	}
}

// StartRoot gates root composition behind the host capability and a host value.
func TestStartRootRejectsInvalidHost(t *testing.T) {
	if err := StartRoot(context.Background(), nil, &rootStoreModule{}); err == nil || err.Error() != "host is required" {
		t.Fatalf("nil host must be rejected: %v", err)
	}
	if err := StartRoot(context.Background(), rootLegacyOnlyHost{}, &rootStoreModule{}); err == nil || !strings.Contains(err.Error(), "root composition") {
		t.Fatalf("legacy-only host must be rejected: %v", err)
	}
}

type rootLegacyOnlyHost struct{}

func (rootLegacyOnlyHost) Start(context.Context, []module.Module) error { return nil }
func (rootLegacyOnlyHost) Ready() bool                                  { return false }
func (rootLegacyOnlyHost) Stop(context.Context) error                   { return nil }

// A module requiring scope kinds must fail startup when the authority
// registry is absent, and nothing may start.
func TestRootRequiredScopeKindsWithoutRegistryFails(t *testing.T) {
	useRootEvents(t)
	m := &rootScopeRequirer{Base: module.Base{Name: "scope-requirer", RequiredScopeKinds: []string{"org.organization"}}}
	if err := StartRoot(context.Background(), newRootTestHost(t), m); err == nil {
		t.Fatal("missing scope registry must fail startup")
	} else if !strings.Contains(err.Error(), "scope registry") {
		t.Fatalf("failure must name the scope registry: %v", err)
	}
	if m.started {
		t.Fatal("no module may start without the scope registry")
	}
}

type rootScopeRequirer struct {
	module.Base
	started bool
}

func (m *rootScopeRequirer) Start(context.Context, module.Runtime) error {
	m.started = true
	return nil
}

// One root instance declares its own provider and replaces it in the same
// window; the replaced factory wins at Start and an empty reason is rejected.
func TestRootBuilderReplacesOwnProvider(t *testing.T) {
	useRootEvents(t)
	m := &rootReplaceProviderModule{}
	if err := StartRoot(context.Background(), newRootTestHost(t), m); err != nil {
		t.Fatal(err)
	}
	if m.saw != "replaced" {
		t.Fatalf("replace option must win at resolve time: %q", m.saw)
	}
}

type rootReplaceProviderModule struct{ saw string }

func (m *rootReplaceProviderModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "replacer"}
}

func (m *rootReplaceProviderModule) Configure(b module.RootBuilder) error {
	provide := func(v string) di.Provider {
		return di.Provider{Key: di.NewKey[string]("replace.value"), Lifetime: di.Singleton,
			Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return v, nil, nil }}
	}
	if err := b.Provide(provide("initial")); err != nil {
		return err
	}
	if err := b.Replace(provide("replaced"), "root replace contract"); err != nil {
		return err
	}
	if err := b.Replace(provide("no-reason"), ""); err == nil {
		return fmt.Errorf("empty replace reason must be rejected")
	}
	return nil
}

func (m *rootReplaceProviderModule) Start(ctx context.Context, rt module.Runtime) error {
	rootEvent("replace:start")
	value, err := rt.Services().Resolve(ctx, di.NewKey[string]("replace.value"))
	if err != nil {
		return err
	}
	m.saw = value.(string)
	return nil
}
