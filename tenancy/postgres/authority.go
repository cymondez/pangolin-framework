// Package postgres implements the local authoritative tenant projection.
package postgres

import (
	"context"
	"embed"
	"errors"
	"strconv"

	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var Migrations embed.FS

// Authority owns no pool: composition closes its injected infrastructure pool.
// Its only operation is a restricted tenant-membership projection query.
type Authority struct{ Pool *pgxpool.Pool }

func (a Authority) Check(ctx context.Context, p scope.PrincipalRef, tenant string) (string, error) {
	if ctx == nil || a.Pool == nil || tenant == "" || p.ID == "" || (p.Kind != "user" && p.Kind != "service") {
		return "", scope.ErrDenied
	}
	var revision int64
	err := a.Pool.QueryRow(ctx, `SELECT revision FROM pangolin_security.tenant_memberships WHERE tenant_id=$1 AND principal_kind=$2 AND principal_id=$3 AND state='active'`, tenant, p.Kind, p.ID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", scope.ErrDenied
	}
	if err != nil {
		return "", errors.Join(scope.ErrAuthorityUnavailable, err)
	}
	if revision <= 0 {
		return "", scope.ErrAuthorityUnavailable
	}
	return strconv.FormatInt(revision, 10), nil
}
