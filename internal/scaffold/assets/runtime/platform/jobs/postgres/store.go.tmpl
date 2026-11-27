// Package postgres implements the durable Jobs queue using PostgreSQL and UOW.
package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/platform/audit"
	"github.com/pangolin-framework/pangolin/platform/jobs"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var Migrations embed.FS

type Store struct {
	pool      *pgxpool.Pool
	registry  *jobs.Registry
	policy    jobs.Policy
	authorize func(context.Context, string) error
}

func NewStore(pool *pgxpool.Pool, registry *jobs.Registry, policy jobs.Policy, authorize func(context.Context, string) error) (*Store, error) {
	if pool == nil || registry == nil || authorize == nil {
		return nil, jobs.ErrInvalid
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Store{pool, registry, policy, authorize}, nil
}

// Enqueue derives ownership and attribution from a trusted binding and participates
// in its business UOW. Capacity is consumed inside the same statement's INSERT via
// trigger-owned permit claims; no global lock or whole-table count is involved.
func (s *Store) Enqueue(ctx context.Context, j jobs.Job) (returned error) {
	defer func() {
		if returned != nil {
			uow.Fail(ctx, returned)
		}
	}()
	b, ok := scope.Current(ctx)
	if !ok || b.Ref != j.Owner || b.Principal != j.Actor {
		return scope.ErrDenied
	}
	if err := s.authorize(ctx, "jobs.enqueue"); err != nil {
		return err
	}
	if err := s.registry.Validate(j); err != nil {
		return err
	}
	ex, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	_, err = ex.Exec(ctx, `WITH cfg AS (SELECT set_config('`+gucJobsCapacity+`','`+strconv.Itoa(s.policy.MaxPending)+`',true),pangolin_jobs.check_capacity_config($9)) INSERT INTO pangolin_jobs.jobs(id,tenant_id,scope_kind,scope_id,document,fingerprint,due_at,deadline) SELECT $1,$2,$3,$4,$5,$6,$7,$8 FROM cfg`, j.ID, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID, raw, hash[:], j.DueAt, j.Deadline, s.policy.MaxPending)
	return capacityOf(err)
}
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func (s *Store) Claim(ctx context.Context, name string) (*jobs.Claim, error) {
	if name == "" {
		return nil, jobs.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	token := rand.Text()
	c := &jobs.Claim{Token: token}
	var raw, hash []byte
	var id string
	var state string
	var ref scope.ScopeRef
	err = tx.QueryRow(ctx, `UPDATE pangolin_jobs.jobs SET
 state=CASE WHEN cancel_requested THEN 'cancelled' WHEN deadline<=now() OR attempts>=$4 THEN 'dead' ELSE 'processing' END,
 lease_token=CASE WHEN cancel_requested OR deadline<=now() OR attempts>=$4 THEN NULL ELSE $1 END,
 locked_by=$2,locked_until=CASE WHEN cancel_requested OR deadline<=now() OR attempts>=$4 THEN NULL ELSE now()+$3*interval '1 microsecond' END,
 finished_at=CASE WHEN cancel_requested OR deadline<=now() OR attempts>=$4 THEN now() ELSE NULL END,
 attempts=CASE WHEN cancel_requested OR deadline<=now() OR attempts>=$4 THEN attempts ELSE attempts+1 END
 WHERE id=(SELECT id FROM pangolin_jobs.jobs WHERE state IN('pending','processing') AND ((state='pending' AND due_at<=now()) OR (state='processing' AND locked_until<=now())) ORDER BY due_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 RETURNING document,fingerprint,attempts,tenant_id,scope_kind,scope_id,id,state`, token, name, s.policy.Lease.Microseconds(), s.policy.MaxAttempts).Scan(&raw, &hash, &c.Attempts, &ref.TenantID, &ref.Kind, &ref.ID, &id, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, tx.Commit(ctx)
	}
	if err != nil {
		return nil, err
	}
	if state != "processing" {
		return nil, tx.Commit(ctx)
	}
	actual := sha256.Sum256(raw)
	if err = json.Unmarshal(raw, &c.Job); err != nil || string(actual[:]) != string(hash) || c.Job.Owner != ref || c.Job.ID != id {
		// Quarantine corrupted documents; do not repeatedly starve the queue.
		_, err = tx.Exec(ctx, `UPDATE pangolin_jobs.jobs SET state='dead',last_error='JOB_DOCUMENT_INVALID',finished_at=now(),lease_token=NULL,locked_until=NULL WHERE lease_token=$1`, token)
		if err != nil {
			return nil, err
		}
		return nil, errors.Join(jobs.ErrInvalid, tx.Commit(ctx))
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return c, nil
}
func (s *Store) Finish(ctx context.Context, c jobs.Claim, success bool, delay time.Duration, code string) error {
	state := "pending"
	if success {
		state = "succeeded"
	} else if c.Attempts >= s.policy.MaxAttempts || !c.Job.Deadline.After(time.Now()) {
		state = "dead"
	}
	// Store safe codes only, never handler errors/payloads.
	if code != "" {
		code = "JOB_EXECUTION_FAILED"
	}
	var final string
	err := s.pool.QueryRow(ctx, `UPDATE pangolin_jobs.jobs SET state=CASE WHEN cancel_requested THEN 'cancelled' ELSE $3 END,due_at=now()+$4*interval '1 microsecond',last_error=$5,lease_token=NULL,locked_until=NULL,finished_at=CASE WHEN cancel_requested OR $3 IN('succeeded','dead') THEN now() ELSE NULL END WHERE id=$1 AND lease_token=$2 AND state='processing' AND locked_until>now() RETURNING state`, c.Job.ID, c.Token, state, delay.Microseconds(), code).Scan(&final)
	if errors.Is(err, pgx.ErrNoRows) {
		return jobs.ErrLeaseLost
	}
	if err == nil && final == "cancelled" {
		return jobs.ErrCancelled
	}
	return err
}
func (s *Store) Cancelled(ctx context.Context, c jobs.Claim) (bool, error) {
	var cancelled bool
	err := s.pool.QueryRow(ctx, `SELECT cancel_requested FROM pangolin_jobs.jobs WHERE id=$1 AND lease_token=$2 AND state='processing' AND locked_until>now()`, c.Job.ID, c.Token).Scan(&cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, jobs.ErrLeaseLost
	}
	return cancelled, err
}
func (s *Store) Release(ctx context.Context, c jobs.Claim) error {
	tag, err := s.pool.Exec(ctx, `UPDATE pangolin_jobs.jobs SET state=CASE WHEN cancel_requested THEN 'cancelled' WHEN attempts >= $3 OR deadline<=now() THEN 'dead' ELSE 'pending' END,due_at=now(),lease_token=NULL,locked_until=NULL,finished_at=CASE WHEN cancel_requested OR attempts >= $3 OR deadline<=now() THEN now() ELSE NULL END WHERE id=$1 AND lease_token=$2 AND state='processing' AND locked_until>now()`, c.Job.ID, c.Token, s.policy.MaxAttempts)
	if err == nil && tag.RowsAffected() != 1 {
		return jobs.ErrLeaseLost
	}
	return err
}

// Cancel and Replay run inside a currently authorized owner UOW, with required
// audit in that same transaction. A replay never changes document or Job ID.
func (s *Store) Cancel(ctx context.Context, id, trace string) error {
	return s.manage(ctx, id, trace, false)
}
func (s *Store) Replay(ctx context.Context, id, trace string) error {
	return s.manage(ctx, id, trace, true)
}
func (s *Store) manage(ctx context.Context, id, trace string, replay bool) (returned error) {
	defer func() {
		if returned != nil {
			uow.Fail(ctx, returned)
		}
	}()
	action := "jobs.cancel"
	if replay {
		action = "jobs.replay"
	}
	if err := s.authorize(ctx, action); err != nil {
		return err
	}
	b, ok := scope.Current(ctx)
	if !ok {
		return scope.ErrDenied
	}
	ex, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	query := `UPDATE pangolin_jobs.jobs SET cancel_requested=true,state=CASE WHEN state='pending' THEN 'cancelled' ELSE state END,finished_at=CASE WHEN state='pending' THEN now() ELSE finished_at END WHERE id=$1 AND tenant_id=$2 AND scope_kind=$3 AND scope_id=$4 AND state IN('pending','processing')`
	if replay {
		query = `UPDATE pangolin_jobs.jobs SET state='pending',cancel_requested=false,attempts=0,due_at=now(),finished_at=NULL,last_error=NULL WHERE id=$1 AND tenant_id=$2 AND scope_kind=$3 AND scope_id=$4 AND state='dead' AND deadline>now()`
	}
	tag, err := ex.Exec(ctx, query, id, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return jobs.ErrInvalid
	}
	return (audit.Recorder{}).Record(ctx, audit.Record{Action: action, Resource: id, Result: "accepted", Trace: trace})
}

// Purge removes terminal records only after the configured safe replay window.
// Dead rows are retained for an operator to reconcile/replay; never silently purged.
func (s *Store) Purge(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM pangolin_jobs.jobs WHERE state IN('succeeded','cancelled') AND finished_at<now()-$1*interval '1 microsecond'`, s.policy.Retention.Microseconds())
	return tag.RowsAffected(), err
}

type Stats struct {
	Pending, Processing, Dead int64
	Oldest                    time.Duration
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	var age float64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='pending'),count(*) FILTER(WHERE state='processing'),count(*) FILTER(WHERE state='dead'),COALESCE(EXTRACT(EPOCH FROM now()-min(created_at) FILTER(WHERE state IN('pending','processing'))),0) FROM pangolin_jobs.jobs`).Scan(&st.Pending, &st.Processing, &st.Dead, &age)
	st.Oldest = time.Duration(age * float64(time.Second))
	return st, err
}
