package uow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cymondez/pangolin-framework/kernel/ambient"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type concurrentResource struct {
	mu      sync.Mutex
	key     string
	rls     bool
	txs     []*concurrentTx
	slotVar *ambient.Var[string]
}

type concurrentTx struct {
	id        int
	mu        sync.Mutex
	commits   atomic.Int32
	rollbacks atomic.Int32
}

func (r *concurrentResource) DatabaseKey() string       { return r.key }
func (r *concurrentResource) RequiresTransaction() bool { return r.rls }
func (r *concurrentResource) Begin(ctx context.Context, _ EffectiveOptions) (context.Context, Transaction, error) {
	r.mu.Lock()
	id := len(r.txs) + 1
	tx := &concurrentTx{id: id}
	r.txs = append(r.txs, tx)
	r.mu.Unlock()
	if r.slotVar != nil {
		ctx = r.slotVar.With(ctx, fmt.Sprintf("tx-%d", id))
		ctx = AttachSlot(ctx, r.slotVar)
	}
	return ctx, tx, nil
}

func (t *concurrentTx) Commit(context.Context) error   { t.commits.Add(1); return nil }
func (t *concurrentTx) Rollback(context.Context) error { t.rollbacks.Add(1); return nil }

func TestConcurrentRequiresNewFromSingleParent(t *testing.T) {
	slot := ambient.New[string]()
	res := &concurrentResource{key: "db-concurrent", rls: true, slotVar: slot}
	m, err := NewManager(res, defaults())
	if err != nil {
		t.Fatal(err)
	}

	const workers = 16
	childErr := errors.New("child-failed")
	err = m.Within(testContext(t), Options{Mode: ReadWrite}, func(parent context.Context) error {
		ps, ok := m.Current(parent)
		if !ok {
			t.Fatal("missing parent scope")
		}
		parentSlot, ok := slot.Current(parent)
		if !ok || parentSlot != "tx-1" {
			t.Fatalf("parent slot = (%q, %v)", parentSlot, ok)
		}

		ready := make(chan struct{}, workers)
		release := make(chan struct{})
		errs := make(chan error, workers)
		ids := make(chan string, workers)
		var wg sync.WaitGroup

		for i := range workers {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				e := m.Within(parent, Options{Mode: ReadWrite, Propagation: RequiresNew}, func(child context.Context) error {
					cs, ok := m.Current(child)
					if !ok {
						return errors.New("missing child scope")
					}
					if cs.ID() == ps.ID() {
						return errors.New("child reused parent root")
					}
					childSlot, ok := slot.Current(child)
					if !ok || childSlot == parentSlot {
						return fmt.Errorf("child inherited parent slot: %q", childSlot)
					}
					if outer, ok := scopeVar.Outer(child); !ok || outer.root.id != ps.ID() {
						return errors.New("child ambient chain lost parent")
					}
					ids <- cs.ID()
					ready <- struct{}{}
					<-release
					if idx%2 == 1 {
						return childErr
					}
					return nil
				})
				if idx%2 == 1 {
					if !errors.Is(e, childErr) {
						errs <- fmt.Errorf("worker %d expected childErr, got %v", idx, e)
					}
				} else if e != nil {
					errs <- fmt.Errorf("worker %d unexpected err: %v", idx, e)
				}
			}(i)
		}

		// Wait until every RequiresNew child is simultaneously active.
		for range workers {
			<-ready
		}
		// Parent itself remains usable while children execute concurrently.
		if err := Check(parent, true); err != nil {
			close(release)
			return fmt.Errorf("parent check failed while children active: %w", err)
		}
		close(release)
		wg.Wait()
		close(errs)
		close(ids)

		for e := range errs {
			return e
		}
		seen := map[string]bool{ps.ID(): true}
		for id := range ids {
			if seen[id] {
				return fmt.Errorf("duplicate scope ID %s", id)
			}
			seen[id] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.txs) != workers+1 {
		t.Fatalf("expected %d transactions, got %d", workers+1, len(res.txs))
	}
	var committed, rolledBack int32
	for _, tx := range res.txs {
		committed += tx.commits.Load()
		rolledBack += tx.rollbacks.Load()
	}
	// Parent + half of children commit; the other half roll back.
	if committed != 1+workers/2 || rolledBack != workers/2 {
		t.Fatalf("outcomes committed=%d rolledBack=%d", committed, rolledBack)
	}
}

func TestConcurrentRequiresNewDepthLimit(t *testing.T) {
	res := &concurrentResource{key: "db-depth", rls: true}
	d := defaults()
	d.MaxRequiresNewDepth = 2
	m, err := NewManager(res, d)
	if err != nil {
		t.Fatal(err)
	}

	const branches = 8
	err = m.Within(testContext(t), Options{Mode: ReadWrite}, func(root context.Context) error {
		// Required child does not consume RequiresNew depth.
		return m.Within(root, Options{Mode: ReadOnly}, func(reqChild context.Context) error {
			var wg sync.WaitGroup
			errs := make(chan error, branches)
			for range branches {
				wg.Add(1)
				go func() {
					defer wg.Done()
					e := m.Within(reqChild, Options{Mode: ReadWrite, Propagation: RequiresNew}, func(d1 context.Context) error {
						return m.Within(d1, Options{Mode: ReadWrite, Propagation: RequiresNew}, func(d2 context.Context) error {
							e := m.Within(d2, Options{Mode: ReadWrite, Propagation: RequiresNew}, func(context.Context) error {
								return errors.New("depth 3 callback should not execute")
							})
							if !errors.Is(e, ErrDepthExceeded) {
								return fmt.Errorf("expected ErrDepthExceeded, got %v", e)
							}
							return nil
						})
					})
					if e != nil {
						errs <- e
					}
				}()
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				return e
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRequiresNewRejectedWhenParentClosedOrCompleting(t *testing.T) {
	var managerRef UnitOfWorkManager
	var completionErr error
	var completingCtx context.Context
	var closedCtx context.Context

	res := &beforeCommitResource{
		key: "db-state",
		beforeCommit: func(c context.Context) error {
			// Parent is in completing state; RequiresNew must fail closed with ErrBusy.
			completionErr = managerRef.Within(completingCtx, Options{Mode: ReadWrite, Propagation: RequiresNew}, func(context.Context) error {
				t.Error("RequiresNew callback ran during parent completion")
				return nil
			})
			return nil
		},
	}
	var err error
	managerRef, err = NewManager(res, defaults())
	if err != nil {
		t.Fatal(err)
	}
	err = managerRef.Within(testContext(t), Options{Mode: ReadWrite}, func(parent context.Context) error {
		completingCtx = parent
		closedCtx = parent
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(completionErr, ErrBusy) {
		t.Fatalf("RequiresNew during completion = %v, want ErrBusy", completionErr)
	}
	if err := managerRef.Within(closedCtx, Options{Mode: ReadWrite, Propagation: RequiresNew}, func(context.Context) error {
		t.Fatal("RequiresNew ran on closed parent")
		return nil
	}); !errors.Is(err, ErrClosed) {
		t.Fatalf("RequiresNew on closed parent = %v, want ErrClosed", err)
	}
}

type beforeCommitResource struct {
	key          string
	beforeCommit func(context.Context) error
}

func (r *beforeCommitResource) DatabaseKey() string       { return r.key }
func (r *beforeCommitResource) RequiresTransaction() bool { return true }
func (r *beforeCommitResource) Begin(ctx context.Context, _ EffectiveOptions) (context.Context, Transaction, error) {
	return ctx, &beforeCommitTx{beforeCommit: r.beforeCommit}, nil
}

type beforeCommitTx struct {
	beforeCommit func(context.Context) error
}

func (t *beforeCommitTx) BeforeCommit(ctx context.Context) error {
	if t.beforeCommit != nil {
		return t.beforeCommit(ctx)
	}
	return nil
}
func (t *beforeCommitTx) Commit(context.Context) error   { return nil }
func (t *beforeCommitTx) Rollback(context.Context) error { return nil }

type countingProvider struct {
	admits  atomic.Int32
	revoked atomic.Bool
}

func (p *countingProvider) Kinds() []string { return []string{"tenant"} }
func (p *countingProvider) Resolve(_ context.Context, principal scope.PrincipalRef, ref scope.ScopeRef) (scope.ScopeBinding, error) {
	n := p.admits.Add(1)
	if p.revoked.Load() {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	return scope.ScopeBinding{Ref: ref, Principal: principal, AuthorityRevision: fmt.Sprintf("rev-%d", n)}, nil
}
func (p *countingProvider) Authorize(context.Context, scope.ScopeBinding, string) error { return nil }

func TestDetachClearsSlotsAndReAdmitsConcurrently(t *testing.T) {
	if Detach(nil) != nil {
		t.Fatal("Detach(nil) != nil")
	}

	provider := &countingProvider{}
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	}
	reg, err := scope.NewRegistry(identity, provider)
	if err != nil {
		t.Fatal(err)
	}
	base, err := reg.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	// Reset admit count after initial Bind.
	provider.admits.Store(0)

	slot := ambient.New[string]()
	res := &concurrentResource{key: "db-detach", rls: true, slotVar: slot}
	d := defaults()
	d.MaxRequiresNewDepth = 1
	m, err := NewManager(res, d)
	if err != nil {
		t.Fatal(err)
	}

	// Second resource without slotVar verifies RequiresNew clears parent's slot.
	bareRes := &concurrentResource{key: "db-bare", rls: true}
	bareM, err := NewManager(bareRes, d)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 10
	err = m.Within(base, Options{Mode: ReadWrite}, func(parent context.Context) error {
		if provider.admits.Load() != 1 {
			t.Fatalf("root admit count = %d, want 1", provider.admits.Load())
		}
		if got, ok := slot.Current(parent); !ok || got != "tx-1" {
			t.Fatalf("parent slot missing: (%q, %v)", got, ok)
		}

		// RequiresNew on a different resource must NOT see parent's attached slot.
		if err := bareM.Within(parent, Options{Mode: ReadWrite, Propagation: RequiresNew}, func(child context.Context) error {
			if got, ok := slot.Current(child); ok {
				return fmt.Errorf("RequiresNew leaked parent slot %q", got)
			}
			return nil
		}); err != nil {
			return err
		}

		// Detach clears UOW scope and all attached resource slots while preserving
		// ScopeBinding and deadline.
		detached := Detach(parent)
		if _, ok := m.Current(detached); ok {
			t.Fatal("detached still has current UOW")
		}
		if err := Check(detached, false); !errors.Is(err, ErrRequired) {
			t.Fatalf("Check(detached) = %v, want ErrRequired", err)
		}
		if _, ok := slot.Current(detached); ok {
			t.Fatal("detached leaked resource slot")
		}
		if b, ok := scope.Current(detached); !ok || b.Ref.TenantID != "tenant-a" {
			t.Fatalf("detached lost ScopeBinding: %+v %v", b, ok)
		}
		if _, ok := detached.Deadline(); !ok {
			t.Fatal("detached lost deadline")
		}
		// Parent itself stays untouched.
		if got, ok := slot.Current(parent); !ok || got != "tx-1" {
			t.Fatalf("Detach mutated parent slot: (%q, %v)", got, ok)
		}

		// Launch concurrent independent top-level UOWs from detached context.
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				e := m.Within(detached, Options{Mode: ReadWrite}, func(top context.Context) error {
					// Since detached starts at depth 0, a nested RequiresNew (depth 1) succeeds.
					return m.Within(top, Options{Mode: ReadWrite, Propagation: RequiresNew}, func(inner context.Context) error {
						if _, ok := m.Current(inner); !ok {
							return errors.New("missing inner scope")
						}
						return nil
					})
				})
				if e != nil {
					errs <- e
				}
			}()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			return e
		}

		// Each top-level UOW from detached re-ran scope.Admit (1 parent + workers detached roots).
		if got := provider.admits.Load(); got != 1+workers {
			return fmt.Errorf("admits = %d, want %d", got, 1+workers)
		}

		// Revocation is enforced on the next top-level UOW from detached.
		provider.revoked.Store(true)
		if err := m.Within(detached, Options{Mode: ReadWrite}, func(context.Context) error {
			return errors.New("revoked detached callback ran")
		}); !errors.Is(err, scope.ErrDenied) {
			return fmt.Errorf("revoked detached = %v, want ErrDenied", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
