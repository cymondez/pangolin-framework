package distributed_test

import (
	"context"
	"errors"
	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"testing"
	"time"
)

type authority struct{ calls int }

func (a *authority) Kinds() []string { return []string{"tenant"} }
func (a *authority) Resolve(_ context.Context, p scope.PrincipalRef, r scope.ScopeRef) (scope.ScopeBinding, error) {
	a.calls++
	revision := "entry"
	if a.calls > 1 {
		revision = "admitted"
	}
	return scope.ScopeBinding{Ref: r, Principal: p, AuthorityRevision: revision}, nil
}
func (a *authority) Authorize(context.Context, scope.ScopeBinding, string) error { return nil }

type resource struct{}

func (resource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return c, transaction{}, nil
}

type transaction struct{}

func (transaction) Commit(context.Context) error   { return nil }
func (transaction) Rollback(context.Context) error { return nil }
func TestExecutionRechecksAdmittedPermission(t *testing.T) {
	for _, kind := range []string{"permission", "delegation"} {
		t.Run(kind, func(t *testing.T) {
			identity := func(context.Context) (scope.PrincipalRef, string, bool) {
				return scope.PrincipalRef{ID: "worker", Kind: "service"}, "a", true
			}
			a := &authority{}
			registry, _ := scope.NewRegistry(identity, a)
			m, _ := uow.NewManager(resource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 1})
			e := distributed.Execution{Manager: m, Identity: identity, Resolver: registry, TrustedProducer: func(context.Context, string) error { return nil }, Authorize: func(c context.Context, _ string) error {
				b, _ := scope.Current(c)
				if kind == "permission" && b.AuthorityRevision == "admitted" {
					return scope.ErrDenied
				}
				return nil
			}, Delegation: func(c context.Context, _ scope.PrincipalRef) error {
				b, _ := scope.Current(c)
				if b.AuthorityRevision == "admitted" {
					return scope.ErrDenied
				}
				return nil
			}}
			task := distributed.Task{Scope: scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"}, Producer: "trusted", Permission: "execute"}
			if kind == "delegation" {
				task.DelegatedPrincipal = &scope.PrincipalRef{ID: "alice", Kind: "user"}
			}
			called := false
			err := e.Run(context.Background(), task, func(context.Context) error { called = true; return nil })
			if called || !errors.Is(err, scope.ErrDenied) {
				t.Fatal("admission reused old permission", called, err)
			}
		})
	}
}
