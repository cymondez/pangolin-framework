// Package postgres composes event completion and PostgreSQL queue stores.
package postgres

import (
	"context"
	"fmt"

	"github.com/pangolin-framework/pangolin/events/local"
	"github.com/pangolin-framework/pangolin/events/outbox"
	"github.com/pangolin-framework/pangolin/persistence/uow"
)

type Resource struct {
	Base      uow.Resource
	Local     *local.Bus
	Publisher *outbox.Publisher
	Before    []func(context.Context) error
}

func (r *Resource) DatabaseKey() string {
	if b, ok := r.Base.(interface{ DatabaseKey() string }); ok {
		return b.DatabaseKey()
	}
	return fmt.Sprintf("events:%p", r)
}
func (r *Resource) RequiresTransaction() bool {
	if b, ok := r.Base.(interface{ RequiresTransaction() bool }); ok {
		return b.RequiresTransaction()
	}
	return true
}
func (r *Resource) Begin(ctx context.Context, o uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	if r.Base == nil {
		return nil, nil, uow.ErrOptionsInvalid
	}
	ctx, tx, err := r.Base.Begin(ctx, o)
	if err != nil {
		return nil, nil, err
	}
	if ctx == nil || tx == nil {
		return nil, nil, uow.ErrOptionsInvalid
	}
	t := &transaction{Transaction: tx, before: append([]func(context.Context) error(nil), r.Before...)}
	if r.Local != nil {
		ctx, t.local = r.Local.Attach(ctx)
	}
	if r.Publisher != nil {
		ctx, t.outbox = r.Publisher.Attach(ctx)
	}
	return ctx, t, nil
}

type transaction struct {
	uow.Transaction
	local  *local.Queue
	outbox *outbox.Queue
	before []func(context.Context) error
}

func (t *transaction) Flush(ctx context.Context) error {
	if f, ok := t.Transaction.(interface{ Flush(context.Context) error }); ok {
		return f.Flush(ctx)
	}
	return nil
}
func (t *transaction) BeforeCommit(ctx context.Context) error {
	if t.local != nil {
		if err := t.local.Drain(ctx, t.Flush); err != nil {
			return err
		}
	}
	if f, ok := t.Transaction.(interface{ BeforeCommit(context.Context) error }); ok {
		if err := f.BeforeCommit(ctx); err != nil {
			return err
		}
	}
	if t.outbox != nil {
		if err := t.outbox.Flush(ctx); err != nil {
			return err
		}
	}
	for _, f := range t.before {
		if f == nil {
			return uow.ErrOptionsInvalid
		}
		if err := f(ctx); err != nil {
			return err
		}
	}
	return nil
}
func (t *transaction) Prepare(ctx context.Context) error {
	if f, ok := t.Transaction.(interface{ Prepare(context.Context) error }); ok {
		return f.Prepare(ctx)
	}
	return nil
}
func (t *transaction) Release(ctx context.Context) error {
	if f, ok := t.Transaction.(interface{ Release(context.Context) error }); ok {
		return f.Release(ctx)
	}
	return nil
}
func (t *transaction) CommitFailureOutcome(err error) uow.Outcome {
	if f, ok := t.Transaction.(interface{ CommitFailureOutcome(error) uow.Outcome }); ok {
		return f.CommitFailureOutcome(err)
	}
	return uow.Unknown
}
