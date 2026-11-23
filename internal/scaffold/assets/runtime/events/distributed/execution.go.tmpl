package distributed

import (
	"context"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// Task carries references, never an old HTTP context or an authority snapshot.
// Jobs may persist this same contract independently of their delivery mechanism.
type Task struct {
	Scope                scope.ScopeRef
	Producer, Permission string
	DelegatedPrincipal   *scope.PrincipalRef
}
type Execution struct {
	Manager         uow.UnitOfWorkManager
	Identity        scope.IdentityReader
	Resolver        scope.ScopeResolver
	TrustedProducer func(context.Context, string) error
	Authorize       func(context.Context, string) error
	Delegation      func(context.Context, scope.PrincipalRef) error
}

func (e Execution) Run(ctx context.Context, task Task, work func(context.Context) error) error {
	if ctx == nil || e.Manager == nil || e.Identity == nil || e.Resolver == nil || e.TrustedProducer == nil || e.Authorize == nil || work == nil || task.Producer == "" || task.Permission == "" || scope.Validate(task.Scope) != nil {
		return scope.ErrDenied
	}
	principal, tenant, trusted := e.Identity(ctx)
	if !trusted || principal.Kind != "service" || principal.ID == "" || tenant != task.Scope.TenantID {
		return scope.ErrDenied
	}
	if err := e.TrustedProducer(ctx, task.Producer); err != nil {
		return err
	}
	bound, err := e.Resolver.Bind(ctx, task.Scope)
	if err != nil {
		return err
	}
	if err = e.Authorize(bound, task.Permission); err != nil {
		return err
	}
	if task.DelegatedPrincipal != nil {
		if e.Delegation == nil {
			return scope.ErrDenied
		}
		if err = e.Delegation(bound, *task.DelegatedPrincipal); err != nil {
			return err
		}
	}
	return e.Manager.Within(bound, uow.Options{Mode: uow.ReadWrite, Transaction: uow.Transactional}, func(admitted context.Context) error {
		if err := e.Authorize(admitted, task.Permission); err != nil {
			return err
		}
		if task.DelegatedPrincipal != nil {
			if err := e.Delegation(admitted, *task.DelegatedPrincipal); err != nil {
				return err
			}
		}
		return work(admitted)
	})
}
