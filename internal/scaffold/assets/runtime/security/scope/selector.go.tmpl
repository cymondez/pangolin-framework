package scope

import (
	"context"
	"errors"
)

var ErrRequired = errors.New("SCOPE_REQUIRED")
var ErrHintConflict = errors.New("SCOPE_HINT_CONFLICT")

// Select resolves exact route/header hints. Empty selectors are allowed only on
// a route explicitly declared tenant-only. A body never chooses the owner.
func Select(tenant string, route, header ScopeRef, explicit bool) (ScopeRef, error) {
	empty := ScopeRef{}
	if route != empty && header != empty && route != header {
		return empty, ErrHintConflict
	}
	ref := route
	if ref == empty {
		ref = header
	}
	if ref == empty {
		if explicit {
			return empty, ErrRequired
		}
		ref = ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant}
	}
	if Validate(ref) != nil {
		return empty, ErrInvalid
	}
	if ref.TenantID != tenant {
		return empty, ErrDenied
	}
	return ref, nil
}
func MatchOwner(ctx context.Context, owner ScopeRef) error {
	b, ok := Current(ctx)
	if !ok {
		return ErrDenied
	}
	if owner != (ScopeRef{}) && owner != b.Ref {
		return ErrDenied
	}
	return nil
}
