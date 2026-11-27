package application

import (
	"context"

	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/policy"
)

// AuthorizedCommand is an explicit sensitive-use-case boundary, including direct
// application calls. ResourcePolicy executes inside the admitted transaction.
func AuthorizedCommand(ctx context.Context, m uow.UnitOfWorkManager, a policy.Authorizer, action string, resource policy.ResourcePolicy, work func(context.Context) error) error {
	if work == nil || isNilInterface(m) || isNilInterface(ctx) {
		return uow.ErrOptionsInvalid
	}
	_, err := AuthorizedCommandResult(ctx, m, a, action, resource, func(c context.Context) (struct{}, error) {
		return struct{}{}, work(c)
	})
	return err
}

// AuthorizedCommandResult authorizes action at entry, opens a ReadWrite
// transactional UOW, enforces resource inside the transaction and returns the
// callback result only after confirmed completion.
func AuthorizedCommandResult[T any](ctx context.Context, m uow.UnitOfWorkManager, a policy.Authorizer, action string, resource policy.ResourcePolicy, work func(context.Context) (T, error)) (T, error) {
	var zero T
	if work == nil || isNilInterface(m) || isNilInterface(ctx) {
		return zero, uow.ErrOptionsInvalid
	}
	if err := policy.Require(ctx, a, action, nil); err != nil {
		return zero, err
	}
	return CommandResult(ctx, m, func(c context.Context) (T, error) {
		if err := policy.Require(c, a, action, resource); err != nil {
			var empty T
			return empty, err
		}
		return work(c)
	})
}

// AuthorizedQuery authorizes action at entry, opens a ReadOnly UOW and enforces
// resource inside the admitted read boundary.
func AuthorizedQuery(ctx context.Context, m uow.UnitOfWorkManager, a policy.Authorizer, action string, resource policy.ResourcePolicy, work func(context.Context) error) error {
	if work == nil || isNilInterface(m) || isNilInterface(ctx) {
		return uow.ErrOptionsInvalid
	}
	_, err := AuthorizedQueryResult(ctx, m, a, action, resource, func(c context.Context) (struct{}, error) {
		return struct{}{}, work(c)
	})
	return err
}

// AuthorizedQueryResult authorizes action at entry, opens a ReadOnly UOW,
// enforces resource inside the read boundary and returns the callback result
// only after confirmed completion.
func AuthorizedQueryResult[T any](ctx context.Context, m uow.UnitOfWorkManager, a policy.Authorizer, action string, resource policy.ResourcePolicy, work func(context.Context) (T, error)) (T, error) {
	var zero T
	if work == nil || isNilInterface(m) || isNilInterface(ctx) {
		return zero, uow.ErrOptionsInvalid
	}
	if err := policy.Require(ctx, a, action, nil); err != nil {
		return zero, err
	}
	return QueryResult(ctx, m, func(c context.Context) (T, error) {
		if err := policy.Require(c, a, action, resource); err != nil {
			var empty T
			return empty, err
		}
		return work(c)
	})
}
