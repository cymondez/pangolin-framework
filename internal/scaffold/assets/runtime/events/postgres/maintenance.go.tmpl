package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	persistence "github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/security/scope"
)

var ErrReplayNotDead = errors.New("EVENT_REPLAY_REQUIRES_DEAD")

type Replay struct {
	Queue, Group, EventID string
	Owner                 scope.ScopeRef
}
type Maintenance struct {
	store     *Store
	authorize func(context.Context, Replay) error
	audit     func(context.Context, Replay) error
}

func NewMaintenance(store *Store, authorize, audit func(context.Context, Replay) error) (*Maintenance, error) {
	if store == nil || authorize == nil || audit == nil {
		return nil, fmt.Errorf("replay authorization and transactional audit required")
	}
	return &Maintenance{store, authorize, audit}, nil
}

// ReplayDead runs inside an authorized write UOW. IDs and envelope remain intact.
func (m *Maintenance) ReplayDead(ctx context.Context, queue, group, id string) error {
	b, ok := scope.Current(ctx)
	if !ok || id == "" || (queue != "outbox" && queue != "inbox") || queue == "inbox" && group == "" {
		return scope.ErrDenied
	}
	change := Replay{Queue: queue, Group: group, EventID: id, Owner: b.Ref}
	if err := m.authorize(ctx, change); err != nil {
		return err
	}
	ex, err := persistence.Current(ctx, true)
	if err != nil {
		return err
	}
	var state string
	if queue == "outbox" {
		err = ex.QueryRow(ctx, `SELECT state FROM pangolin_events.outbox WHERE event_id=$1 AND tenant_id=$2 AND scope_kind=$3 AND scope_id=$4 FOR UPDATE`, id, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID).Scan(&state)
	} else {
		err = ex.QueryRow(ctx, `SELECT state FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2 AND tenant_id=$3 AND scope_kind=$4 AND scope_id=$5 FOR UPDATE`, group, id, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID).Scan(&state)
	}
	if err != nil {
		return err
	}
	if state != "dead" {
		return ErrReplayNotDead
	}
	if err = m.audit(ctx, change); err != nil {
		return err
	}
	if queue == "outbox" {
		_, err = ex.Exec(ctx, `UPDATE pangolin_events.outbox SET state='pending',attempts=0,next_attempt_at=now(),lease_token=NULL,locked_until=NULL,last_error=NULL WHERE event_id=$1 AND state='dead'`, id)
	} else {
		_, err = ex.Exec(ctx, `UPDATE pangolin_events.inbox SET state='pending',attempts=0,next_attempt_at=now(),version=version+1 WHERE consumer_group=$1 AND event_id=$2 AND state='dead'`, group, id)
	}
	return err
}

type Retention struct{ Broker, ManualReplay, Inbox, OutboxSent time.Duration }

func (r Retention) Validate() error {
	if r.Broker <= 0 || r.ManualReplay <= 0 || r.Inbox < r.Broker || r.Inbox < r.ManualReplay || r.OutboxSent <= 0 {
		return fmt.Errorf("inbox retention must cover broker and manual replay windows")
	}
	return nil
}

// Purge removes only confirmed terminal rows outside the declared guarantee.
// It is an infrastructure task, never an operation result or a replay API.
func (s *Store) Purge(ctx context.Context, retention Retention) (int64, error) {
	if err := retention.Validate(); err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)
	out, err := tx.Exec(ctx, `DELETE FROM pangolin_events.outbox WHERE state='sent' AND sent_at<now()-$1*interval '1 microsecond'`, retention.OutboxSent.Microseconds())
	if err != nil {
		return 0, err
	}
	in, err := tx.Exec(ctx, `DELETE FROM pangolin_events.inbox WHERE state='processed' AND processed_at<now()-$1*interval '1 microsecond'`, retention.Inbox.Microseconds())
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return out.RowsAffected() + in.RowsAffected(), nil
}
