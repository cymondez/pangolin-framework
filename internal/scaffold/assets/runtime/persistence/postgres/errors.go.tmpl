package postgres

import (
	"context"
	"errors"

	"github.com/cymondez/pangolin-framework/persistence/database"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CurrentWithScope validates and returns both the guarded pgx Executor for the
// requested access mode and the verified trusted ScopeRef from ctx.
func CurrentWithScope(ctx context.Context, write bool) (*Executor, scope.ScopeRef, error) {
	ex, err := Current(ctx, write)
	if err != nil {
		return nil, scope.ScopeRef{}, err
	}
	owner, err := database.CurrentOwner(ctx)
	if err != nil {
		return nil, scope.ScopeRef{}, err
	}
	return ex, owner.Ref(), nil
}

// IsUniqueViolation reports whether err wraps a PostgreSQL unique_violation
// error (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// MapError maps well-known PostgreSQL driver errors onto neutral database
// sentinels while preserving the original cause for errors.Is and errors.As.
// pgx.ErrNoRows maps to database.ErrNoRows; SQLSTATE 23505 maps to
// database.ErrConflict; nil and unrecognized errors are returned unchanged.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if errors.Is(err, database.ErrNoRows) {
			return err
		}
		return errors.Join(database.ErrNoRows, err)
	}
	if IsUniqueViolation(err) {
		if errors.Is(err, database.ErrConflict) {
			return err
		}
		return errors.Join(database.ErrConflict, err)
	}
	return err
}
