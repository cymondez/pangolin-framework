package database

import (
	"context"
	"errors"

	"github.com/pangolin-framework/pangolin/security/scope"
)

// ErrConflict is the neutral sentinel for a unique or conflict constraint
// violation mapped by backend adapters.
var ErrConflict = errors.New("DATABASE_CONFLICT")

// IsNotFound reports whether err represents a missing row (ErrNoRows / sql.ErrNoRows).
func IsNotFound(err error) bool { return errors.Is(err, ErrNoRows) }

// IsConflict reports whether err represents a backend conflict (ErrConflict).
func IsConflict(err error) bool { return errors.Is(err, ErrConflict) }

// CurrentWithScope validates and returns both the active guarded neutral
// Executor and the verified trusted ScopeRef from ctx. Either missing or
// invalid fails closed.
func CurrentWithScope(ctx context.Context) (Executor, scope.ScopeRef, error) {
	ex, err := Current(ctx)
	if err != nil {
		return nil, scope.ScopeRef{}, err
	}
	owner, err := CurrentOwner(ctx)
	if err != nil {
		return nil, scope.ScopeRef{}, err
	}
	return ex, owner.Ref(), nil
}
