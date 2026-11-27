package module

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/pangolin-framework/pangolin/kernel/di"
)

type scopeContract interface{ Do() string }

type scopeImpV1 struct{}

func (scopeImpV1) Do() string { return "v1" }

type scopeImpV2 struct{}

func (scopeImpV2) Do() string { return "v2" }

func newScopeImpV1() scopeImpV1 { return scopeImpV1{} }

func newScopeImpV2() scopeImpV2 { return scopeImpV2{} }

// scopePortPrivate is a private (never-exported) service owned by the port
// module: even a direct dependent must not resolve it.
type scopePortPrivate struct{}

func newScopePortPrivate() *scopePortPrivate { return &scopePortPrivate{} }

type scopeOutsiderPrivate struct{}

func newScopeOutsiderPrivate() *scopeOutsiderPrivate { return &scopeOutsiderPrivate{} }

type scopeOpService struct{ id int }

// scopeOpRecorder counts actual factory runs and per-instance cleanups.
type scopeOpRecorder struct {
	mu      sync.Mutex
	runs    int
	cleaned map[int]int
}

func newScopeOpRecorder() *scopeOpRecorder {
	return &scopeOpRecorder{cleaned: map[int]int{}}
}

func (r *scopeOpRecorder) factory() func() (*scopeOpService, di.Cleanup, error) {
	return func() (*scopeOpService, di.Cleanup, error) {
		r.mu.Lock()
		r.runs++
		id := r.runs
		r.mu.Unlock()
		return &scopeOpService{id: id}, func(context.Context) error {
			r.mu.Lock()
			r.cleaned[id]++
			r.mu.Unlock()
			return nil
		}, nil
	}
}

func (r *scopeOpRecorder) assertRuns(t *testing.T, want int) {
	t.Helper()
	r.mu.Lock()
	got := r.runs
	r.mu.Unlock()
	if got != want {
		t.Fatalf("factory runs = %d, want %d", got, want)
	}
}

func (r *scopeOpRecorder) assertCleanedTotal(t *testing.T, want int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, n := range r.cleaned {
		total += n
	}
	if total != want {
		t.Fatalf("cleanup count = %d (%v), want %d", total, r.cleaned, want)
	}
}

func (r *scopeOpRecorder) assertCleanedExactlyOnce(t *testing.T, id int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cleaned[id] != 1 {
		t.Fatalf("cleanup for instance %d ran %d times, want exactly 1", id, r.cleaned[id])
	}
}

// Fixture modules: explicit ModuleMetadata methods so the loader-created zero
// instances of dependencies keep stable declared names.
type scopeRootPortModule struct{}

func (*scopeRootPortModule) ModuleMetadata() Metadata {
	return Metadata{Name: "scopeport", Version: "0.1.0"}
}

type scopeRootAppModule struct{}

func (*scopeRootAppModule) ModuleMetadata() Metadata {
	return Metadata{Name: "scopeapp", Version: "0.1.0"}
}

func (*scopeRootAppModule) DependsOn() []Ref { return []Ref{Of[scopeRootPortModule]()} }

type scopeRootImplModule struct{}

func (*scopeRootImplModule) ModuleMetadata() Metadata {
	return Metadata{Name: "scopeimpl", Version: "0.1.0"}
}

func (*scopeRootImplModule) DependsOn() []Ref { return []Ref{Of[scopeRootPortModule]()} }

type scopeRootOutsiderModule struct{}

func (*scopeRootOutsiderModule) ModuleMetadata() Metadata {
	return Metadata{Name: "scopeoutsider", Version: "0.1.0"}
}

func newRootScopeComposition(t *testing.T) Composition {
	t.Helper()
	plan, err := LoadRoot(&scopeRootAppModule{}, &scopeRootImplModule{}, &scopeRootOutsiderModule{})
	if err != nil {
		t.Fatalf("LoadRoot: %v", err)
	}
	return newComposition(t, plan)
}

func scopeAppResolver(t *testing.T, c Composition) di.Resolver {
	t.Helper()
	resolver, err := c.LifecycleResolver("scopeapp")
	if err != nil {
		t.Fatalf("LifecycleResolver(scopeapp): %v", err)
	}
	return resolver
}

func registerScopePrivateFixtures(t *testing.T, c Composition) {
	t.Helper()
	register(t, c, "scopeport", func(s Services) {
		if err := s.Add(newScopePortPrivate); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "scopeoutsider", func(s Services) {
		if err := s.Add(newScopeOutsiderPrivate); err != nil {
			t.Fatal(err)
		}
	})
}

// scopeWrappedResolver hides the lifecycle resolver's concrete type behind
// another interface: OpenScope's type assertion must reject it, since a
// delegating wrapper cannot prove which owner's visibility it would carry.
type scopeWrappedResolver struct{ inner di.Resolver }

func (w scopeWrappedResolver) Resolve(ctx context.Context, key di.Token) (any, error) {
	return w.inner.Resolve(ctx, key)
}

func TestScopeOperationIdentityCleanupAndBoundaries(t *testing.T) {
	c := newRootScopeComposition(t)
	rec := newScopeOpRecorder()
	register(t, c, "scopeapp", func(s Services) {
		if err := s.Add(rec.factory(), Operation()); err != nil {
			t.Fatal(err)
		}
	})
	registerScopePrivateFixtures(t, c)
	container := seal(t, c)
	resolver := scopeAppResolver(t, c)
	ctx := context.Background()

	if _, err := resolver.Resolve(ctx, Service[*scopeOpService]()); err == nil {
		t.Fatal("plain lifecycle Resolve must still reject Operation without an explicit scope")
	}
	if _, err := resolver.Resolve(ctx, Service[*scopePortPrivate]()); err == nil {
		t.Fatal("lifecycle resolve of a private foreign service must be rejected")
	}

	scope, err := OpenScope(ctx, resolver)
	if err != nil {
		t.Fatalf("OpenScope: %v", err)
	}
	if _, ok := any(scope).(di.Container); ok {
		t.Fatal("operation scope must never expose the raw container")
	}
	rec.assertRuns(t, 0)
	rec.assertCleanedTotal(t, 0)
	if _, err := OpenScope(ctx, container); err == nil {
		t.Fatal("raw container must not be accepted as a resolver for OpenScope")
	}
	if _, err := OpenScope(nil, resolver); err == nil {
		t.Fatal("nil context must be rejected")
	}
	scope2, err := OpenScope(ctx, resolver)
	if err != nil {
		t.Fatalf("second independent OpenScope for the same resolver: %v", err)
	}
	if err := scope2.Close(ctx); err != nil {
		t.Fatalf("second scope close: %v", err)
	}
	rec.assertRuns(t, 0)
	rec.assertCleanedTotal(t, 0)

	first, err := scope.Resolve(ctx, Service[*scopeOpService]())
	if err != nil {
		t.Fatalf("scoped resolve: %v", err)
	}
	second, err := scope.Resolve(ctx, Service[*scopeOpService]())
	if err != nil {
		t.Fatalf("second scoped resolve: %v", err)
	}
	if first.(*scopeOpService) != second.(*scopeOpService) {
		t.Fatal("Operation identity must be stable within one scope")
	}
	rec.assertRuns(t, 1)
	rec.assertCleanedTotal(t, 0)

	if _, err := scope.Resolve(ctx, Service[*scopePortPrivate]()); err == nil {
		t.Fatal("private port-owned service must stay invisible behind a direct dependency")
	} else {
		mustErrContains(t, err, "scopeapp", "cannot resolve", "scopeport")
	}
	if _, err := scope.Resolve(ctx, Service[*scopeOutsiderPrivate]()); err == nil {
		t.Fatal("foreign non-direct service must be rejected")
	} else {
		mustErrContains(t, err, "scopeapp", "cannot resolve", "scopeoutsider")
	}
	if _, err := scope.Resolve(ctx, nil); err == nil {
		t.Fatal("nil token must be rejected")
	}
	if _, err := scope.Resolve(ctx, di.NewKey[*scopeOpService]("")); err == nil {
		t.Fatal("empty-name token must be rejected")
	}
	if _, err := scope.Resolve(nil, Service[*scopeOpService]()); err == nil {
		t.Fatal("nil resolve context must be rejected")
	}

	if err := scope.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	rec.assertCleanedExactlyOnce(t, first.(*scopeOpService).id)
	rec.assertCleanedTotal(t, 1)
	if err := scope.Close(ctx); err != nil {
		t.Fatalf("second Close must be idempotent: %v", err)
	}
	rec.assertCleanedExactlyOnce(t, first.(*scopeOpService).id)
	rec.assertCleanedTotal(t, 1)
	if _, err := scope.Resolve(ctx, Service[*scopeOpService]()); err == nil {
		t.Fatal("resolve must fail after the scope is closed")
	} else {
		mustErrContains(t, err, "closed")
	}
}

func TestScopeOperationDistinctAcrossScopes(t *testing.T) {
	c := newRootScopeComposition(t)
	rec := newScopeOpRecorder()
	register(t, c, "scopeapp", func(s Services) {
		if err := s.Add(rec.factory(), Operation()); err != nil {
			t.Fatal(err)
		}
	})
	seal(t, c)
	resolver := scopeAppResolver(t, c)
	ctx := context.Background()

	scopeA, err := OpenScope(ctx, resolver)
	if err != nil {
		t.Fatalf("OpenScope A: %v", err)
	}
	scopeB, err := OpenScope(ctx, resolver)
	if err != nil {
		t.Fatalf("OpenScope B: %v", err)
	}
	a, err := scopeA.Resolve(ctx, Service[*scopeOpService]())
	if err != nil {
		t.Fatalf("resolve in A: %v", err)
	}
	b, err := scopeB.Resolve(ctx, Service[*scopeOpService]())
	if err != nil {
		t.Fatalf("resolve in B: %v", err)
	}
	if a.(*scopeOpService).id == b.(*scopeOpService).id {
		t.Fatal("Operation instances must differ across scopes")
	}
	aID := a.(*scopeOpService).id
	bID := b.(*scopeOpService).id

	if err := scopeA.Close(ctx); err != nil {
		t.Fatalf("Close A: %v", err)
	}
	rec.assertCleanedExactlyOnce(t, aID)
	rec.mu.Lock()
	cleanedB := rec.cleaned[bID]
	rec.mu.Unlock()
	if cleanedB != 0 {
		t.Fatal("closing scope A must not clean scope B's instance")
	}
	again, err := scopeB.Resolve(ctx, Service[*scopeOpService]())
	if err != nil {
		t.Fatalf("resolve in B after A closed: %v", err)
	}
	if again.(*scopeOpService) != b.(*scopeOpService) {
		t.Fatal("scope B identity must survive an independent scope's close")
	}
	rec.assertRuns(t, 2)
	if err := scopeB.Close(ctx); err != nil {
		t.Fatalf("Close B: %v", err)
	}
	rec.assertCleanedExactlyOnce(t, bID)
}

func TestScopeConcurrentResolveRunsFactoryOnce(t *testing.T) {
	c := newRootScopeComposition(t)
	rec := newScopeOpRecorder()
	entered := make(chan struct{})
	var enteredOnce sync.Once
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(closeRelease)
	register(t, c, "scopeapp", func(s Services) {
		if err := s.Add(func() (*scopeOpService, di.Cleanup, error) {
			enteredOnce.Do(func() {
				rec.mu.Lock()
				rec.runs++
				rec.mu.Unlock()
				close(entered)
			})
			<-release
			return &scopeOpService{id: 1}, func(context.Context) error {
				rec.mu.Lock()
				rec.cleaned[1]++
				rec.mu.Unlock()
				return nil
			}, nil
		}, Operation()); err != nil {
			t.Fatal(err)
		}
	})
	seal(t, c)
	resolver := scopeAppResolver(t, c)
	scope, err := OpenScope(context.Background(), resolver)
	if err != nil {
		t.Fatalf("OpenScope: %v", err)
	}
	t.Cleanup(func() { _ = scope.Close(context.Background()) })

	const workers = 8
	var (
		start   = make(chan struct{})
		ready   = make(chan struct{}, workers)
		values  = make(chan any, workers)
		failure = make(chan error, workers)
		done    sync.WaitGroup
	)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			ready <- struct{}{}
			<-start
			v, err := scope.Resolve(context.Background(), Service[*scopeOpService]())
			if err != nil {
				failure <- err
				return
			}
			values <- v
		}()
	}
	for i := 0; i < workers; i++ {
		<-ready
	}
	close(start)
	select {
	case err := <-failure:
		closeRelease()
		t.Fatalf("concurrent resolve failed: %v", err)
	case <-entered:
	}
	rec.assertRuns(t, 1)
	closeRelease()
	done.Wait()
	rec.assertRuns(t, 1)
	rec.assertCleanedTotal(t, 0)
	for i := 0; i < workers; i++ {
		v := <-values
		if v.(*scopeOpService).id != 1 {
			t.Fatalf("all resolvers must see the single Operation instance, got id %d", v.(*scopeOpService).id)
		}
	}
}

func TestScopeResolverGuardRejections(t *testing.T) {
	c := newRootScopeComposition(t)
	rec := newScopeOpRecorder()
	register(t, c, "scopeapp", func(s Services) {
		if err := s.Add(rec.factory(), Operation()); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	resolver := scopeAppResolver(t, c)
	ctx := context.Background()

	if _, err := OpenScope(ctx, nil); err == nil {
		t.Fatal("nil resolver must be rejected")
	}
	var typedNil *lifecycleResolver
	if _, err := OpenScope(ctx, typedNil); err == nil {
		t.Fatal("typed-nil lifecycle resolver must be rejected")
	}
	if _, err := OpenScope(ctx, scopeWrappedResolver{inner: resolver}); err == nil {
		t.Fatal("a delegating wrapper must not launder ownership past the type check")
	}
	if _, err := OpenScope(ctx, container); err == nil {
		t.Fatal("the raw container must not be accepted")
	}
	if _, err := OpenScope(nil, resolver); err == nil {
		t.Fatal("nil context must be rejected")
	}

	engine := c.(*composition)
	revealed := &lifecycleResolver{c: engine, owner: "ghost"}
	if _, err := OpenScope(ctx, revealed); err == nil {
		t.Fatal("nonexistent owner must be rejected")
	} else {
		mustErrContains(t, err, "composition has no module ghost")
	}
	unsealed := newComposition(t, planOf(Module{Name: "lonely"}))
	lonely := &lifecycleResolver{c: unsealed.(*composition), owner: "lonely"}
	if _, err := OpenScope(ctx, lonely); err == nil {
		t.Fatal("unsealed composition must not open scopes")
	} else {
		mustErrContains(t, err, "composition is not sealed")
	}
	inner, err := container.OpenScope(ctx)
	if err != nil {
		t.Fatalf("fabrication setup: %v", err)
	}
	fabricated := &moduleScope{c: engine, owner: "ghost", inner: inner}
	if _, err := fabricated.Resolve(ctx, Service[*scopeOpService]()); err == nil {
		t.Fatal("fabricated unknown-owner scope must be rejected")
	} else {
		mustErrContains(t, err, "composition has no module ghost")
	}
	if err := fabricated.Close(ctx); err != nil {
		t.Fatalf("fabrication teardown: %v", err)
	}
}

func TestScopeDirectContractNeutralReplacement(t *testing.T) {
	c := newRootScopeComposition(t)
	register(t, c, "scopeport", func(s Services) {
		if err := s.Export(Contract[scopeContract]()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "scopeimpl", func(s Services) {
		if err := s.Add(newScopeImpV1, As(Contract[scopeContract]())); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "scopeport", func(s Services) {
		if err := s.ReplaceContract(Contract[scopeContract](), newScopeImpV2, "owner upgrade over the neutral contribution"); err != nil {
			t.Fatal(err)
		}
	})
	seal(t, c)
	resolver := scopeAppResolver(t, c)
	ctx := context.Background()

	scope, err := OpenScope(ctx, resolver)
	if err != nil {
		t.Fatalf("OpenScope: %v", err)
	}
	t.Cleanup(func() { _ = scope.Close(context.Background()) })

	lifecycle, err := resolver.Resolve(ctx, Contract[scopeContract]())
	if err != nil {
		t.Fatalf("direct contract resolve through lifecycle resolver: %v", err)
	}
	if got := lifecycle.(scopeContract).Do(); got != "v2" {
		t.Fatalf("lifecycle resolver sees %q, want replacement implementation v2", got)
	}
	scoped, err := scope.Resolve(ctx, Contract[scopeContract]())
	if err != nil {
		t.Fatalf("direct contract resolve through scope: %v", err)
	}
	if got := scoped.(scopeContract).Do(); got != "v2" {
		t.Fatalf("scope resolver sees %q, want replacement implementation v2", got)
	}
	log := strings.Join(c.ReplacementLog(), "\n")
	if !strings.Contains(log, "replaced contract") || !strings.Contains(log, "owner upgrade over the neutral contribution") {
		t.Fatalf("neutral replacement provenance missing: %s", log)
	}
}

func TestScopeCancelAndClosedContainer(t *testing.T) {
	c := newRootScopeComposition(t)
	rec := newScopeOpRecorder()
	register(t, c, "scopeapp", func(s Services) {
		if err := s.Add(rec.factory(), Operation()); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	resolver := scopeAppResolver(t, c)
	ctx := context.Background()

	openCtx, cancel := context.WithCancel(context.Background())
	scope, err := OpenScope(openCtx, resolver)
	if err != nil {
		t.Fatalf("OpenScope: %v", err)
	}
	created, err := scope.Resolve(ctx, Service[*scopeOpService]())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	rec.assertCleanedTotal(t, 0)
	cancel()
	if _, err := scope.Resolve(ctx, Service[*scopeOpService]()); err == nil {
		t.Fatal("canceling the open context must invalidate the scope")
	} else {
		mustErrContains(t, err, "closed")
	}
	if err := scope.Close(ctx); err != nil {
		t.Fatalf("Close after cancel: %v", err)
	}
	rec.assertCleanedExactlyOnce(t, created.(*scopeOpService).id)

	canceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := OpenScope(canceled, resolver); err == nil {
		t.Fatal("an already canceled context must be refused")
	} else {
		mustErrContains(t, err, "canceled")
	}

	parked, err := OpenScope(ctx, resolver)
	if err != nil {
		t.Fatalf("OpenScope before container close: %v", err)
	}
	parkedValue, err := parked.Resolve(ctx, Service[*scopeOpService]())
	if err != nil {
		t.Fatalf("resolve before container close: %v", err)
	}
	if err := container.Close(ctx); err != nil {
		t.Fatalf("container Close: %v", err)
	}
	rec.assertCleanedExactlyOnce(t, parkedValue.(*scopeOpService).id)
	if _, err := OpenScope(ctx, resolver); err == nil {
		t.Fatal("closed container must refuse new operation scopes")
	} else {
		mustErrContains(t, err, "closed")
	}
	if _, err := parked.Resolve(ctx, Service[*scopeOpService]()); err == nil {
		t.Fatal("resolve must fail after the container closed the scope")
	} else {
		mustErrContains(t, err, "closed")
	}
	if err := parked.Close(ctx); err != nil {
		t.Fatalf("closing a scope the container already closed must be safe: %v", err)
	}
	rec.assertCleanedExactlyOnce(t, parkedValue.(*scopeOpService).id)
}
