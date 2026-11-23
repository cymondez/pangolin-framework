package cache

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/security/scope"
)

func bound(t *testing.T, tenant, kind, id, user, revision string) context.Context {
	t.Helper()
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: user, Kind: "user"}, tenant, true
	}
	r, err := scope.NewRegistry(identity, fixtureProvider{tenant, kind, id, user, revision})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := r.Bind(context.Background(), scope.ScopeRef{TenantID: tenant, Kind: kind, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

type fixtureProvider struct{ tenant, kind, id, user, revision string }

func (p fixtureProvider) Kinds() []string { return []string{p.kind} }
func (p fixtureProvider) Resolve(_ context.Context, principal scope.PrincipalRef, ref scope.ScopeRef) (scope.ScopeBinding, error) {
	return scope.ScopeBinding{Ref: ref, Principal: principal, AuthorityRevision: p.revision}, nil
}
func (fixtureProvider) Authorize(context.Context, scope.ScopeBinding, string) error { return nil }
func TestA65Cache(t *testing.T) {
	t.Run("tenant-cache-isolation", func(t *testing.T) {
		m, _ := NewMemory("app", "0.1.0", 8)
		r := Resource{Module: "catalog", Kind: "book", ID: "same"}
		a, b := bound(t, "a", "tenant", "a", "u", "1"), bound(t, "b", "tenant", "b", "u", "1")
		if err := m.Set(a, r, []byte("a"), time.Minute); err != nil {
			t.Fatal(err)
		}
		if _, hit, _ := m.Get(b, r); hit {
			t.Fatal("tenant leak")
		}
		v, _, _ := m.Get(a, r)
		v[0] = 'x'
		v, _, _ = m.Get(a, r)
		if string(v) != "a" {
			t.Fatal("mutable cache")
		}
	})
	t.Run("ttl-version-convergence", func(t *testing.T) {
		m, _ := NewMemory("app", "v", 2)
		now := time.Now()
		m.now = func() time.Time { return now }
		ctx := bound(t, "a", "tenant", "a", "u", "1")
		r := Resource{Module: "m", Kind: "r", ID: "id"}
		_ = m.Set(ctx, r, []byte("old"), time.Second)
		now = now.Add(time.Second)
		if _, hit, _ := m.Get(ctx, r); hit {
			t.Fatal("expired hit")
		}
		v, err := m.Load(ctx, r, time.Second, func(context.Context) ([]byte, error) { return []byte("new"), nil })
		if err != nil || string(v) != "new" {
			t.Fatal(v, err)
		}
	})
}
func TestA80Cache(t *testing.T) {
	a, b := bound(t, "t", "org.organization", "a", "u", "1"), bound(t, "t", "org.organization", "b", "u", "1")
	m, _ := NewMemory("app", "v", 8)
	r := Resource{Module: "m", Kind: "r", ID: "id"}
	t.Run("scope-cache-isolation", func(t *testing.T) {
		_ = m.Set(a, r, []byte("a"), time.Hour)
		if _, hit, _ := m.Get(b, r); hit {
			t.Fatal("scope leak")
		}
		for _, ctx := range []context.Context{bound(t, "t", "org.enterprise", "a", "u", "1"), bound(t, "t", "tenant", "t", "u", "1")} {
			if _, hit, _ := m.Get(ctx, r); hit {
				t.Fatal("kind leak")
			}
		}
		r.Private = true
		_ = m.Set(a, r, []byte("private"), time.Hour)
		for _, ctx := range []context.Context{bound(t, "t", "org.organization", "a", "other", "1"), bound(t, "t", "org.organization", "a", "u", "2")} {
			if _, hit, _ := m.Get(ctx, r); hit {
				t.Fatal("principal/revision leak")
			}
		}
		r.Private = false
	})
	t.Run("scoped-invalidation", func(t *testing.T) {
		_ = m.Set(a, r, []byte("a"), time.Hour)
		_ = m.Set(b, r, []byte("b"), time.Hour)
		_ = m.Delete(a, r)
		if _, hit, _ := m.Get(a, r); hit {
			t.Fatal("not invalidated")
		}
		if v, hit, _ := m.Get(b, r); !hit || string(v) != "b" {
			t.Fatal("cross-scope invalidation")
		}
	})
}
func TestMemoryConcurrency(t *testing.T) {
	m, _ := NewMemory("app", "v", 2)
	ctx := bound(t, "t", "tenant", "t", "u", "1")
	r := Resource{Module: "m", Kind: "r", ID: "i"}
	entered, release := make(chan struct{}), make(chan struct{})
	var loads atomic.Int32
	var wg sync.WaitGroup
	load := func(context.Context) ([]byte, error) {
		if loads.Add(1) == 1 {
			close(entered)
		}
		<-release
		return []byte("v"), nil
	}
	wg.Go(func() { _, _ = m.Load(ctx, r, time.Hour, load) })
	<-entered
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.Load(cancelled, r, time.Second, load); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for range 16 {
		wg.Go(func() {
			v, err := m.Load(ctx, r, time.Hour, load)
			if err != nil || string(v) != "v" {
				t.Error(err)
			}
		})
	}
	deadline := time.Now().Add(time.Second)
	for m.Stats().Coalesced < 16 {
		if time.Now().After(deadline) {
			t.Fatal("waiters did not join flight")
		}
		runtime.Gosched()
	}
	waiterCtx, cancelWaiter := context.WithCancel(ctx)
	waiterDone := make(chan error, 1)
	wg.Go(func() { _, err := m.Load(waiterCtx, r, time.Hour, load); waiterDone <- err })
	for m.Stats().Coalesced < 17 {
		if time.Now().After(deadline) {
			t.Fatal("cancellable waiter did not join")
		}
		runtime.Gosched()
	}
	cancelWaiter()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	wg.Wait()
	if loads.Load() != 1 {
		t.Fatal(loads.Load())
	}
	_ = m.Delete(ctx, r)
	start, finish := make(chan struct{}), make(chan struct{})
	wg.Go(func() {
		_, _ = m.Load(ctx, r, time.Hour, func(context.Context) ([]byte, error) { close(start); <-finish; return []byte("stale"), nil })
	})
	<-start
	_ = m.Delete(ctx, r)
	close(finish)
	wg.Wait()
	if _, hit, _ := m.Get(ctx, r); hit {
		t.Fatal("invalidated fill resurrected")
	}
	for _, id := range []string{"1", "2", "3"} {
		r.ID = id
		_ = m.Set(ctx, r, []byte(id), time.Hour)
	}
	if m.Stats().Evictions == 0 {
		t.Fatal("unbounded cache")
	}
}

type publisherFunc func(context.Context, Invalidation) error

func (f publisherFunc) Publish(c context.Context, e Invalidation) error { return f(c, e) }
func TestInvalidationEvents(t *testing.T) {
	a, b := bound(t, "t", "org.organization", "a", "u", "1"), bound(t, "t", "org.organization", "b", "u", "1")
	var event Invalidation
	sentinel := errors.New("bus offline")
	m, _ := NewMemory("app", "v", 8, publisherFunc(func(_ context.Context, e Invalidation) error { event = e; return sentinel }))
	r := Resource{Module: "m", Kind: "r", ID: "id", Private: true}
	_ = m.Set(a, r, []byte("a"), time.Hour)
	_ = m.Set(b, r, []byte("b"), time.Hour)
	if err := m.Delete(a, r); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if event.Scope != (scope.ScopeRef{TenantID: "t", Kind: "org.organization", ID: "a"}) {
		t.Fatal(event)
	}
	_ = m.Set(a, r, []byte("a"), time.Hour)
	if err := m.Apply(b, event); !errors.Is(err, scope.ErrDenied) {
		t.Fatal("cross-scope event", err)
	}
	if err := m.Apply(a, event); err != nil {
		t.Fatal(err)
	}
	other, _ := NewMemory("other", "v", 8)
	if err := other.Apply(a, event); !errors.Is(err, scope.ErrDenied) {
		t.Fatal("cross-application event", err)
	}
	if _, hit, _ := m.Get(a, r); hit {
		t.Fatal("event missed private variant")
	}
	if _, hit, _ := m.Get(b, r); !hit {
		t.Fatal("event crossed scope")
	}
}

func TestScopedFillInvalidation(t *testing.T) {
	a, b := bound(t, "t", "org.organization", "a", "u", "1"), bound(t, "t", "org.organization", "b", "u", "1")
	m, _ := NewMemory("app", "v", 8)
	r := Resource{Module: "m", Kind: "r", ID: "id"}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Load(b, r, time.Hour, func(context.Context) ([]byte, error) { close(entered); <-release; return []byte("b"), nil })
	}()
	<-entered
	if err := m.Delete(a, r); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if value, hit, _ := m.Get(b, r); !hit || string(value) != "b" {
		t.Fatal("another scope's fill was invalidated")
	}
	_ = m.Delete(b, r)
	entered, release, done = make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Load(b, r, time.Hour, func(context.Context) ([]byte, error) { close(entered); <-release; return []byte("old"), nil })
	}()
	<-entered
	_ = m.Delete(b, r)
	value, err := m.Load(b, r, time.Hour, func(context.Context) ([]byte, error) { return []byte("new"), nil })
	if err != nil || string(value) != "new" {
		t.Fatal(value, err)
	}
	close(release)
	<-done
	if value, hit, _ := m.Get(b, r); !hit || string(value) != "new" {
		t.Fatal("old fill overwrote new fill")
	}
}
func TestCacheErrors(t *testing.T) {
	if _, err := NewMemory("", "v", 1); err == nil {
		t.Fatal("invalid config")
	}
	m, _ := NewMemory("a", "v", 1)
	ctx := bound(t, "t", "tenant", "t", "u", "1")
	r := Resource{Module: "m", Kind: "r", ID: "id"}
	if _, _, err := m.Get(context.Background(), r); err == nil {
		t.Fatal("unbound")
	}
	if err := m.Set(ctx, r, nil, 0); err == nil {
		t.Fatal("ttl")
	}
	bad := r
	bad.ID = ""
	if _, err := Key(ctx, "a", "v", bad); err == nil {
		t.Fatal("key")
	}
	if _, err := m.Load(ctx, r, 0, nil); err == nil {
		t.Fatal("loader")
	}
	sentinel := errors.New("source")
	if _, err := m.Load(ctx, r, time.Second, func(context.Context) ([]byte, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic swallowed")
			}
		}()
		_, _ = m.Load(ctx, r, time.Second, func(context.Context) ([]byte, error) { panic("loader") })
	}()
	if len(m.flights) != 0 {
		t.Fatal("flight leaked")
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	if m.Delete(c, r) == nil || m.Set(c, r, nil, time.Second) == nil {
		t.Fatal("cancel")
	}
}
