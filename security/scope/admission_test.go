package scope_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
)

type changingAuthority struct {
	revision string
	denied   bool
	calls    int
}

func (a *changingAuthority) Kinds() []string { return []string{"org.organization"} }
func (a *changingAuthority) Resolve(_ context.Context, p scope.PrincipalRef, r scope.ScopeRef) (scope.ScopeBinding, error) {
	a.calls++
	if a.denied {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	return scope.ScopeBinding{Ref: r, Principal: p, AuthorityRevision: a.revision}, nil
}
func (a *changingAuthority) Authorize(context.Context, scope.ScopeBinding, string) error { return nil }

type resource struct{ begins int }

func (r *resource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	r.begins++
	return c, transaction{}, nil
}

type transaction struct{}

func (transaction) Commit(context.Context) error   { return nil }
func (transaction) Rollback(context.Context) error { return nil }

func TestA88Admission(t *testing.T) {
	a := &changingAuthority{revision: "1"}
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	}
	registry, _ := scope.NewRegistry(identity, a)
	ref := scope.ScopeRef{TenantID: "tenant-a", Kind: "org.organization", ID: "org-a"}
	bound, err := registry.Bind(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	r := &resource{}
	m, _ := uow.NewManager(r, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	t.Run("revocation-between-bind-and-admission", func(t *testing.T) {
		a.denied = true
		err := m.Within(bound, uow.Options{}, func(context.Context) error { t.Fatal("revoked callback"); return nil })
		if !errors.Is(err, scope.ErrDenied) || r.begins != 0 {
			t.Fatal(err, r.begins)
		}
	})
	t.Run("admitted-operation-snapshot", func(t *testing.T) {
		a.denied = false
		a.revision = "2"
		err := m.Within(bound, uow.Options{}, func(ctx context.Context) error {
			b, _ := scope.Current(ctx)
			if b.AuthorityRevision != "2" {
				t.Fatal(b)
			}
			before := a.calls
			a.denied = true
			if err := m.Within(ctx, uow.Options{Propagation: uow.RequiresNew}, func(child context.Context) error {
				b, _ := scope.Current(child)
				if b.AuthorityRevision != "2" {
					t.Fatal(b)
				}
				return nil
			}); err != nil {
				return err
			}
			if a.calls != before {
				t.Fatal("RequiresNew extended authorization")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("new-operation-after-revocation", func(t *testing.T) {
		if err := m.Within(bound, uow.Options{}, func(context.Context) error { t.Fatal("stale authorization"); return nil }); !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
	})
}
