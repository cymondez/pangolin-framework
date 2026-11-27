package application

import (
	"context"
	"errors"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"testing"
	"time"
)

type authorizer func(context.Context, string) error

func (f authorizer) Authorize(c context.Context, a string) error { return f(c, a) }

type authorizedResource struct{ begins int }

func (r *authorizedResource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	r.begins++
	return c, authorizedTx{}, nil
}

type authorizedTx struct{}

func (authorizedTx) Commit(context.Context) error   { return nil }
func (authorizedTx) Rollback(context.Context) error { return nil }
func TestA13DirectUseCase(t *testing.T) {
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "a", true
	})
	ctx, _ := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"})
	r := &authorizedResource{}
	m, _ := uow.NewManager(r, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	allowed := false
	checks, work := 0, 0
	a := authorizer(func(context.Context, string) error {
		checks++
		if !allowed {
			return scope.ErrDenied
		}
		return nil
	})
	callback := func(context.Context) error { work++; return nil }
	if err := AuthorizedCommand(ctx, m, a, "books.write", nil, callback); !errors.Is(err, scope.ErrDenied) || r.begins != 0 || work != 0 {
		t.Fatal(err, r.begins, work)
	}
	allowed = true
	if err := AuthorizedCommand(ctx, m, a, "books.write", func(c context.Context) error {
		if err := uow.Check(c, true); err != nil {
			t.Fatal(err)
		}
		return scope.ErrDenied
	}, callback); !errors.Is(err, scope.ErrDenied) || work != 0 {
		t.Fatal(err, work)
	}
	if err := AuthorizedCommand(ctx, m, a, "books.write", nil, callback); err != nil || work != 1 || checks != 5 {
		t.Fatal(err, checks, work)
	}
}
