// Package application provides explicit policies shared by HTTP, jobs and consumers.
package application

import (
	"context"
	"github.com/cymondez/pangolin-framework/observability"
	"github.com/cymondez/pangolin-framework/persistence/uow"
)

// Command establishes the same write policy for every transport.
func Command(ctx context.Context, m uow.UnitOfWorkManager, callback func(context.Context) error) (err error) {
	if m == nil {
		return uow.ErrOptionsInvalid
	}
	if ctx != nil {
		var finish func(error)
		ctx, finish = observability.Start(ctx, "use_case")
		defer func() { finish(err) }()
	}
	return m.Within(ctx, uow.Options{Mode: uow.ReadWrite, Transaction: uow.Transactional}, callback)
}

// Query explicitly restricts data access; an RLS adapter opens a read transaction.
func Query(ctx context.Context, m uow.UnitOfWorkManager, callback func(context.Context) error) (err error) {
	if m == nil {
		return uow.ErrOptionsInvalid
	}
	if ctx != nil {
		var finish func(error)
		ctx, finish = observability.Start(ctx, "use_case")
		defer func() { finish(err) }()
	}
	return m.Within(ctx, uow.Options{Mode: uow.ReadOnly}, callback)
}
