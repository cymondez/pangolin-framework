// Package postgres helpers map deployment capacity admission to typed errors.
package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/cymondez/pangolin-framework/platform/jobs"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrCapacityUnprovisioned   error = capacityUnprovisionedError{}
	ErrCapacityConfigConflict  error = capacityConfigConflictError{}
	ErrCapacityRequired        error = capacityRequiredError{}
	ErrCapacityUpgradeRequired error = capacityUpgradeRequiredError{}
)

type capacityUnprovisionedError struct{}

func (capacityUnprovisionedError) Error() string   { return "JOB_CAPACITY_UNPROVISIONED" }
func (capacityUnprovisionedError) Code() string    { return "JOB_CAPACITY_UNPROVISIONED" }
func (capacityUnprovisionedError) HTTPStatus() int { return 503 }

type capacityConfigConflictError struct{}

func (capacityConfigConflictError) Error() string   { return "JOB_CAPACITY_CONFIG_CONFLICT" }
func (capacityConfigConflictError) Code() string    { return "JOB_CAPACITY_CONFIG_CONFLICT" }
func (capacityConfigConflictError) HTTPStatus() int { return 503 }

// ErrCapacityRequired refuses an unbounded caller against a provisioned queue.
type capacityRequiredError struct{}

func (capacityRequiredError) Error() string   { return "JOB_CAPACITY_REQUIRED" }
func (capacityRequiredError) Code() string    { return "JOB_CAPACITY_REQUIRED" }
func (capacityRequiredError) HTTPStatus() int { return 503 }

type capacityUpgradeRequiredError struct{}

func (capacityUpgradeRequiredError) Error() string   { return "JOB_CAPACITY_UPGRADE_REQUIRED" }
func (capacityUpgradeRequiredError) Code() string    { return "JOB_CAPACITY_UPGRADE_REQUIRED" }
func (capacityUpgradeRequiredError) HTTPStatus() int { return 503 }

const (
	errCodeCapacityExceeded = "JC001"
	errCodeUnprovisioned    = "JC002"
	errCodeConfigConflict   = "JC003"
	errCodeCapacityRequired = "JC004"
	errCodeUpgradeRequired  = "JC005"
	gucJobsCapacity         = "pangolin.capacity_jobs"
	provisionStatement      = `SELECT pangolin_jobs.provision_permits($1,$2)`
)

// capacityOf maps the database admission contract to typed errors. The jobs
// table only raises these codes through the trigger-owned permit functions.
func capacityOf(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case errCodeCapacityExceeded:
			return jobs.ErrCapacity
		case errCodeUnprovisioned:
			return ErrCapacityUnprovisioned
		case errCodeConfigConflict:
			return ErrCapacityConfigConflict
		case errCodeCapacityRequired:
			return ErrCapacityRequired
		case errCodeUpgradeRequired:
			return ErrCapacityUpgradeRequired
		}
	}
	return err
}

// ProvisionQueue runs the deployment-only jobs permit provisioning on the
// migration connection after the jobs Migrations were applied. action "install"
// refuses an already provisioned queue; "resize" is the idempotent deployment
// entry and creates the permit pool on first use. Both run under queue and
// permit table locks, adopt existing rows transactionally, and refuse targets
// below live rows. Call it from deployment with the configured MaxPending,
// never on request paths.
func ProvisionQueue(ctx context.Context, db *sql.DB, capacity int, action string) error {
	if db == nil {
		return errors.New("provisioning requires migration database")
	}
	if action != "install" && action != "resize" {
		return errors.New("provisioning action must be install or resize")
	}
	if capacity <= 0 {
		return jobs.ErrCapacity
	}
	if _, err := db.ExecContext(ctx, provisionStatement, capacity, action); err != nil {
		return err
	}
	return nil
}
