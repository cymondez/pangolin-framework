package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/cymondez/pangolin-framework/events/outbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrCapacity error = capacityError{}

type capacityError struct{}

func (capacityError) Error() string   { return "EVENT_CAPACITY_EXCEEDED" }
func (capacityError) Code() string    { return "EVENT_CAPACITY_EXCEEDED" }
func (capacityError) HTTPStatus() int { return 429 }

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
