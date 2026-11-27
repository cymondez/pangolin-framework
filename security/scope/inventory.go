package scope

import (
	"context"
	"errors"
)

var ErrMigrationRequired = errors.New("SCOPE_MIGRATION_REQUIRED")

type Inventory interface {
	RequiredKinds(context.Context) ([]string, error)
}

// CheckInventory runs before Ready against a complete deployment-owned inventory
// of catalog, business owners and pending work. No data is rewritten or dropped.
func (r *Registry) CheckInventory(ctx context.Context, inventory Inventory) error {
	if ctx == nil || inventory == nil {
		return ErrAuthorityUnavailable
	}
	kinds, err := inventory.RequiredKinds(ctx)
	if err != nil {
		return errors.Join(ErrAuthorityUnavailable, err)
	}
	for _, kind := range kinds {
		if r.providers[kind] == nil {
			return errors.Join(ErrMigrationRequired, ErrKindUnsupported)
		}
	}
	return nil
}
