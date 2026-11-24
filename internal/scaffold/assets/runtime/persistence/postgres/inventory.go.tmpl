package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OwnerTable is an explicit infrastructure descriptor; business repositories
// cannot disable filters. Inventory's pool is a restricted metadata query Port.
type OwnerTable struct{ Schema, Table, KindColumn string }
type ScopeInventory struct {
	Pool   *pgxpool.Pool
	Tables []OwnerTable
}

func (i ScopeInventory) RequiredKinds(ctx context.Context) ([]string, error) {
	if i.Pool == nil || len(i.Tables) == 0 {
		return nil, errors.New("complete scope inventory required")
	}
	seen := map[string]bool{}
	result := []string{}
	for _, t := range i.Tables {
		if t.Schema == "" || t.Table == "" || t.KindColumn == "" {
			return nil, errors.New("invalid scope inventory table")
		}
		// An ordinary RLS-filtered application connection cannot prove the absence
		// of other owners. Fail closed instead of accepting a partial inventory.
		var filtered bool
		if err := i.Pool.QueryRow(ctx, `SELECT row_security_active($1::regclass)`, pgx.Identifier{t.Schema, t.Table}.Sanitize()).Scan(&filtered); err != nil {
			return nil, err
		}
		if filtered {
			return nil, errors.New("scope inventory requires complete metadata visibility")
		}
		rows, err := i.Pool.Query(ctx, `SELECT DISTINCT `+pgx.Identifier{t.KindColumn}.Sanitize()+` FROM `+pgx.Identifier{t.Schema, t.Table}.Sanitize())
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k string
			if err = rows.Scan(&k); err != nil {
				rows.Close()
				return nil, err
			}
			if k == "" {
				rows.Close()
				return nil, errors.New("scope kind missing in persistent data")
			}
			if !seen[k] {
				seen[k] = true
				result = append(result, k)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
