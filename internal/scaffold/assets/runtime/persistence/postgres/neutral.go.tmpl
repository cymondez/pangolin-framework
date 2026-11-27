package postgres

import (
	"context"

	"github.com/pangolin-framework/pangolin/persistence/database"
	"github.com/pangolin-framework/pangolin/persistence/uow"
)

// Capabilities is truthful: PostgreSQL enforces transaction-local FORCE RLS,
// engine read-only transactions and all three explicit isolation levels. The
// returned snapshot is detached.
func (r *Resource) Capabilities() database.Capabilities {
	return database.Capabilities{
		Backend:             "postgres",
		RowLevelSecurity:    true,
		TransactionReadOnly: true,
		Isolations:          []uow.Isolation{uow.ReadCommitted, uow.RepeatableRead, uow.Serializable},
	}
}

// Dialect returns the PostgreSQL SQL generation rules.
func (r *Resource) Dialect() database.Dialect { return database.DollarDialect }

// neutralBinder is installed by Begin next to the guarded pgx executor. Every
// database.Current call binds a fresh view to the caller's operation context,
// mirroring postgres.Current: a Required child context inherits the same
// transaction and receives its own view, an ended view fails closed forever,
// and a view is never rebound to another session.
type neutralBinder struct {
	key string
}

func (b *neutralBinder) DatabaseKey() string { return b.key }

func (b *neutralBinder) Bind(ctx context.Context) (database.Executor, error) {
	return &neutralView{key: b.key, work: ctx}, nil
}

// neutralView captures the operation context it was bound to. Its lifetime
// and strict identity are checked before every delegation, so an old view
// can never operate under a different active context; each statement then
// goes through the guarded pgx executor of the same unit of work.
type neutralView struct {
	key  string
	work context.Context
}

func (v *neutralView) DatabaseKey() string { return v.key }

func (v *neutralView) enter(ctx context.Context, write bool) error {
	if err := uow.Check(v.work, write); err != nil {
		return err
	}
	if ctx != v.work {
		return uow.Fail(v.work, uow.ErrScopeMismatch)
	}
	return nil
}

func (v *neutralView) Exec(ctx context.Context, query string, args ...any) (database.ExecResult, error) {
	if err := v.enter(ctx, true); err != nil {
		return database.ExecResult{}, err
	}
	ex, err := Current(ctx, true)
	if err != nil {
		return database.ExecResult{}, err
	}
	tag, err := ex.Exec(ctx, query, args...)
	if err != nil {
		return database.ExecResult{}, err
	}
	return database.ExecResult{RowsAffected: tag.RowsAffected()}, nil
}

func (v *neutralView) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	if err := v.enter(ctx, false); err != nil {
		return nil, err
	}
	ex, err := Current(ctx, false)
	if err != nil {
		return nil, err
	}
	return ex.Query(ctx, query, args...)
}

func (v *neutralView) QueryWrite(ctx context.Context, query string, args ...any) (database.Rows, error) {
	if err := v.enter(ctx, true); err != nil {
		return nil, err
	}
	ex, err := Current(ctx, true)
	if err != nil {
		return nil, err
	}
	return ex.Query(ctx, query, args...)
}

func (v *neutralView) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	if err := v.enter(ctx, false); err != nil {
		return failedNeutralRow{err}
	}
	ex, err := Current(ctx, false)
	if err != nil {
		return failedNeutralRow{err}
	}
	return ex.QueryRow(ctx, query, args...)
}

func (v *neutralView) QueryRowWrite(ctx context.Context, query string, args ...any) database.Row {
	if err := v.enter(ctx, true); err != nil {
		return failedNeutralRow{err}
	}
	ex, err := Current(ctx, true)
	if err != nil {
		return failedNeutralRow{err}
	}
	return ex.QueryRow(ctx, query, args...)
}

type failedNeutralRow struct{ err error }

func (r failedNeutralRow) Scan(...any) error { return r.err }

var _ database.Resource = (*Resource)(nil)
var _ database.Binder = (*neutralBinder)(nil)
var _ database.Executor = (*neutralView)(nil)
