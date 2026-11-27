package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"time"

	"github.com/pangolin-framework/pangolin/events/outbox"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrCapacity                error = capacityError{}
	ErrCapacityUnprovisioned   error = capacityUnprovisionedError{}
	ErrCapacityConfigConflict  error = capacityConfigConflictError{}
	ErrCapacityRequired        error = capacityRequiredError{}
	ErrCapacityUpgradeRequired error = capacityUpgradeRequiredError{}
)

type capacityError struct{}

func (capacityError) Error() string   { return "EVENT_CAPACITY_EXCEEDED" }
func (capacityError) Code() string    { return "EVENT_CAPACITY_EXCEEDED" }
func (capacityError) HTTPStatus() int { return 429 }

type capacityUnprovisionedError struct{}

func (capacityUnprovisionedError) Error() string   { return "EVENT_CAPACITY_UNPROVISIONED" }
func (capacityUnprovisionedError) Code() string    { return "EVENT_CAPACITY_UNPROVISIONED" }
func (capacityUnprovisionedError) HTTPStatus() int { return 503 }

type capacityConfigConflictError struct{}

func (capacityConfigConflictError) Error() string   { return "EVENT_CAPACITY_CONFIG_CONFLICT" }
func (capacityConfigConflictError) Code() string    { return "EVENT_CAPACITY_CONFIG_CONFLICT" }
func (capacityConfigConflictError) HTTPStatus() int { return 503 }

// ErrCapacityRequired refuses an unbounded caller against a provisioned queue.
type capacityRequiredError struct{}

func (capacityRequiredError) Error() string   { return "EVENT_CAPACITY_REQUIRED" }
func (capacityRequiredError) Code() string    { return "EVENT_CAPACITY_REQUIRED" }
func (capacityRequiredError) HTTPStatus() int { return 503 }

type capacityUpgradeRequiredError struct{}

func (capacityUpgradeRequiredError) Error() string   { return "EVENT_CAPACITY_UPGRADE_REQUIRED" }
func (capacityUpgradeRequiredError) Code() string    { return "EVENT_CAPACITY_UPGRADE_REQUIRED" }
func (capacityUpgradeRequiredError) HTTPStatus() int { return 503 }

//go:embed capacity_migrations/*.sql
var CapacityMigrations embed.FS

type Capacity struct{ Outbox, Inbox int }

func NewBoundedStore(pool *pgxpool.Pool, policy outbox.Policy, capacity Capacity) (*Store, error) {
	if capacity.Outbox <= 0 || capacity.Inbox <= 0 {
		return nil, ErrCapacity
	}
	s, err := NewStore(pool, policy)
	if err != nil {
		return nil, err
	}
	s.capacity = capacity
	return s, nil
}

type QueueStats struct {
	Pending, Processing, Dead int64
	Oldest                    time.Duration
}

func (s *Store) Stats(ctx context.Context, queue string) (QueueStats, error) {
	var st QueueStats
	var age float64
	query := `SELECT count(*) FILTER(WHERE state='pending'),count(*) FILTER(WHERE state='processing'),count(*) FILTER(WHERE state='dead'),COALESCE(EXTRACT(EPOCH FROM now()-min(created_at) FILTER(WHERE state IN('pending','processing'))),0) FROM pangolin_events.outbox`
	if queue == "inbox" {
		query = `SELECT count(*) FILTER(WHERE state='pending'),0,count(*) FILTER(WHERE state='dead'),COALESCE(EXTRACT(EPOCH FROM now()-min(created_at) FILTER(WHERE state='pending')),0) FROM pangolin_events.inbox`
	} else if queue != "outbox" {
		return st, ErrCapacity
	}
	err := s.pool.QueryRow(ctx, query).Scan(&st.Pending, &st.Processing, &st.Dead, &age)
	st.Oldest = time.Duration(age * float64(time.Second))
	return st, err
}

const (
	errCodeCapacityExceeded = "PC001"
	errCodeUnprovisioned    = "PC002"
	errCodeConfigConflict   = "PC003"
	errCodeCapacityRequired = "PC004"
	errCodeUpgradeRequired  = "PC005"
	gucOutboxCapacity       = "pangolin.capacity_outbox"
	gucInboxCapacity        = "pangolin.capacity_inbox"
	queueOutbox             = "outbox"
	queueInbox              = "inbox"
	provisionStatement      = `SELECT pangolin_events.provision_queue_permits($1,$2,$3)`
)

// capacityOf maps the database admission contract to typed errors. The queue
// tables only raise these codes through the trigger-owned permit functions.
func capacityOf(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case errCodeCapacityExceeded:
			return ErrCapacity
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

// ProvisionQueues runs the deployment-only permit provisioning on the migration
// connection after CapacityMigrations were applied. action "install" refuses an
// already provisioned queue; "resize" is the idempotent deployment entry and
// creates the permit pool on first use. Both queues are provisioned in ONE
// transaction under queue and permit table locks, adopt existing rows
// transactionally, refuse targets below live rows, and roll back completely so
// both queues stay consistent on any failure. Call it from deployment with the
// configured capacities, never on request paths.
func ProvisionQueues(ctx context.Context, db *sql.DB, outboxCapacity, inboxCapacity int, action string) error {
	if db == nil {
		return errors.New("provisioning requires migration database")
	}
	if action != "install" && action != "resize" {
		return errors.New("provisioning action must be install or resize")
	}
	if outboxCapacity <= 0 || inboxCapacity <= 0 {
		return ErrCapacity
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		return errors.Join(cause, tx.Rollback())
	}
	if _, err = tx.ExecContext(ctx, provisionStatement, queueOutbox, outboxCapacity, action); err != nil {
		return rollback(err)
	}
	if _, err = tx.ExecContext(ctx, provisionStatement, queueInbox, inboxCapacity, action); err != nil {
		return rollback(err)
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}
