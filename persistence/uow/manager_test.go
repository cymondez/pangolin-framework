package uow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/security/scope"
)

type testResource struct {
	tx  *testTx
	err error
}
type testTx struct {
	commitErr, rollbackErr error
	commits, rollbacks     int
}

func (r testResource) Begin(ctx context.Context, _ EffectiveOptions) (context.Context, Transaction, error) {
	return ctx, r.tx, r.err
}
func (t *testTx) Commit(context.Context) error   { t.commits++; return t.commitErr }
func (t *testTx) Rollback(context.Context) error { t.rollbacks++; return t.rollbackErr }
func testContext(t *testing.T) context.Context {
	t.Helper()
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	ctx, e := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if e != nil {
		t.Fatal(e)
	}
	return ctx
}
func defaults() DefaultOptions {
	return DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 4}
}
func TestRequiredTransactionLifecycle(t *testing.T) {
	ctx := testContext(t)
	tx := &testTx{}
	m, _ := NewManager(testResource{tx: tx}, defaults())
	var escaped context.Context
	var saved Scope
	if _, ok := m.Current(nil); ok {
		t.Fatal("nil current")
	}
	if Check(nil, false) == nil {
		t.Fatal("missing scope")
	}
	completed, disposed := 0, 0
	e := m.Within(ctx, Options{Mode: ReadWrite}, func(work context.Context) error {
		escaped = work
		s, ok := m.Current(work)
		if !ok || s.ID() == "" || s.Options().Mode != ReadWrite {
			t.Fatal("scope")
		}
		saved = s
		if e := s.SaveChanges(work); e != nil {
			return e
		}
		if tx.commits != 0 {
			t.Fatal("flush commits")
		}
		_ = s.OnCompleted(func(hook context.Context, c Completion) error {
			completed++
			if c.Outcome != Committed || Check(hook, false) == nil {
				t.Fatal("hook executor/outcome")
			}
			return nil
		})
		_ = s.OnFailed(func(context.Context, Completion) error { t.Fatal("failed hook after success"); return nil })
		_ = s.OnDisposed(func(context.Context, Completion) error { disposed++; return nil })
		return m.Within(work, Options{Mode: ReadOnly, Timeout: time.Millisecond * 500}, func(child context.Context) error {
			cs, _ := m.Current(child)
			if cs.ID() != s.ID() || cs.Options().Mode != ReadOnly {
				t.Fatal("child options")
			}
			return Check(child, false)
		})
	})
	if e != nil || tx.commits != 1 || tx.rollbacks != 0 || completed != 1 || disposed != 1 {
		t.Fatal(e, tx, completed, disposed)
	}
	if _, ok := m.Current(escaped); ok {
		t.Fatal("escaped current")
	}
	if Check(escaped, false) == nil || saved.SetRollbackOnly(nil) == nil || saved.OnCompleted(func(context.Context, Completion) error { return nil }) == nil {
		t.Fatal("ended scope")
	}
}

func TestExpiredRequiredChildDoesNotPoisonActiveRoot(t *testing.T) {
	ctx := testContext(t)
	tx := &testTx{}
	m, _ := NewManager(testResource{tx: tx}, defaults())
	var expired context.Context
	err := m.Within(ctx, Options{Mode: ReadWrite}, func(root context.Context) error {
		if err := m.Within(root, Options{Mode: ReadOnly}, func(child context.Context) error { expired = child; return nil }); err != nil {
			return err
		}
		if err := m.Within(expired, Options{Mode: ReadOnly}, func(context.Context) error { t.Fatal("expired child callback ran"); return nil }); !errors.Is(err, ErrClosed) {
			t.Fatalf("expired child reentry: %v", err)
		}
		return nil
	})
	if err != nil || tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("stale view poisoned live root: err=%v tx=%+v", err, tx)
	}
}
func TestRollbackUnknownPanicAndHooks(t *testing.T) {
	cause := errors.New("injected")
	ctx := testContext(t)
	for _, mode := range []string{"callback", "rollback-only", "nested-error", "nested-options", "readonly", "cancel", "panic", "commit-unknown", "hook-error", "nil-hook", "binding-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			tx := &testTx{}
			m, _ := NewManager(testResource{tx: tx}, defaults())
			if mode == "commit-unknown" {
				tx.commitErr = cause
			}
			failed := 0
			invoke := func() error {
				return m.Within(ctx, Options{Mode: ReadWrite}, func(work context.Context) error {
					s, _ := m.Current(work)
					_ = s.OnFailed(func(context.Context, Completion) error { failed++; return nil })
					switch mode {
					case "callback":
						return cause
					case "rollback-only":
						_ = s.SetRollbackOnly(nil)
						return s.SetRollbackOnly(cause)
					case "nested-error":
						_ = m.Within(work, Options{}, func(context.Context) error { return cause })
						return nil
					case "nested-options":
						_ = m.Within(work, Options{Timeout: -1}, func(context.Context) error { t.Fatal("invalid callback"); return nil })
						return nil
					case "readonly":
						return m.Within(work, Options{Mode: ReadOnly}, func(child context.Context) error { return Check(child, true) })
					case "cancel":
						cancelled, cancel := context.WithCancel(work)
						cancel()
						return Check(cancelled, false)
					case "panic":
						panic(cause)
					case "hook-error":
						return s.OnCompleted(func(context.Context, Completion) error { return cause })
					case "nil-hook":
						return s.OnDisposed(nil)
					case "binding-mismatch":
						other := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
							return scope.PrincipalRef{ID: "mallory", Kind: "user"}, "tenant-a", true
						})
						bad, _ := other.Bind(work, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
						return Check(bad, false)
					}
					return nil
				})
			}
			if mode == "panic" {
				func() {
					defer func() {
						if recover() != cause {
							t.Fatal("panic not rethrown")
						}
					}()
					_ = invoke()
				}()
			} else {
				err := invoke()
				if mode == "hook-error" {
					if err != nil || tx.commits != 1 {
						t.Fatal(err, tx)
					}
					return
				}
				var failure Error
				if !errors.As(err, &failure) {
					t.Fatal(err)
				}
				if mode == "commit-unknown" && failure.Outcome() != Unknown {
					t.Fatal("lost unknown")
				}
				if mode == "hook-error" && failure.Outcome() != Committed {
					t.Fatal("lost commit")
				}
				if failure.Code() == "" || failure.ScopeID() == "" || failure.OperationID() == "" || failure.Error() == "" {
					t.Fatal("error identity")
				}
				_ = failure.Unwrap()
			}
			if mode == "hook-error" {
				if tx.rollbacks != 0 || failed != 0 {
					t.Fatal(tx, failed)
				}
			} else if tx.rollbacks != 1 || failed != 1 {
				t.Fatal(tx, failed)
			}
		})
	}
}
func TestOptionAndBeginRejections(t *testing.T) {
	if _, e := NewManager(nil, defaults()); e == nil {
		t.Fatal("resource")
	}
	if _, e := NewManager(testResource{}, DefaultOptions{}); e == nil {
		t.Fatal("defaults")
	}
	ctx := testContext(t)
	m, _ := NewManager(testResource{tx: &testTx{}}, defaults())
	for _, o := range []Options{{Mode: 99}, {Transaction: 99}, {Transaction: NonTransactional}, {Propagation: 99}, {Isolation: 99}, {Timeout: -1}} {
		if e := m.Within(ctx, o, func(context.Context) error { t.Fatal("invalid callback"); return nil }); e == nil {
			t.Fatal(o)
		}
	}
	if e := m.Within(nil, Options{}, func(context.Context) error { return nil }); e == nil {
		t.Fatal("nil context")
	}
	if e := m.Within(ctx, Options{}, nil); e == nil {
		t.Fatal("nil callback")
	}
	if e := m.Within(context.Background(), Options{}, func(context.Context) error { return nil }); e == nil {
		t.Fatal("missing binding")
	}
	m, _ = NewManager(testResource{err: errors.New("begin failed")}, defaults())
	if e := m.Within(ctx, Options{}, func(context.Context) error { t.Fatal("begin failed"); return nil }); e == nil {
		t.Fatal("begin error")
	}
}
