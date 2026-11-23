package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"errors"
	"log/slog"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/events/inbox"
	"github.com/cymondez/pangolin-framework/events/outbox"
	persistence "github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var Migrations embed.FS

type Store struct {
	pool   *pgxpool.Pool
	policy outbox.Policy
}

func NewStore(pool *pgxpool.Pool, policy outbox.Policy) (*Store, error) {
	if pool == nil {
		return nil, outbox.ErrUnavailable
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Store{pool, policy}, nil
}
func (s *Store) Append(ctx context.Context, e distributed.Envelope, raw []byte) error {
	encoded, encodeErr := distributed.Encode(e)
	if encodeErr != nil || !bytes.Equal(encoded, raw) {
		return errors.Join(inbox.ErrContentConflict, encodeErr)
	}
	b, ok := scope.Current(ctx)
	if !ok || b.Ref != (scope.ScopeRef{TenantID: e.TenantID, Kind: e.ScopeKind, ID: e.ScopeID}) {
		return scope.ErrDenied
	}
	ex, err := persistence.Current(ctx, true)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `INSERT INTO pangolin_events.outbox(event_id,tenant_id,scope_kind,scope_id,envelope) VALUES($1,$2,$3,$4,$5)`, e.ID, e.TenantID, e.ScopeKind, e.ScopeID, raw)
	return err
}
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func (s *Store) Claim(ctx context.Context, worker string, p outbox.Policy) (*outbox.Claim, error) {
	if worker == "" || p.Validate() != nil || p != s.policy {
		return nil, outbox.ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	dead, err := tx.Exec(ctx, `UPDATE pangolin_events.outbox SET state='dead',lease_token=NULL,locked_until=NULL,last_error='MAX_ATTEMPTS' WHERE attempts >= $1 AND ((state='processing' AND locked_until<=now()) OR state='pending')`, p.MaxAttempts)
	if err != nil {
		return nil, err
	}
	if dead.RowsAffected() > 0 {
		slog.ErrorContext(ctx, "outbox exhausted delivery attempts", "dead_count", dead.RowsAffected())
	}
	token := rand.Text()
	var raw []byte
	var attempt int
	err = tx.QueryRow(ctx, `UPDATE pangolin_events.outbox SET state='processing',lease_token=$1,locked_by=$2,locked_until=now()+$3*interval '1 microsecond',attempts=attempts+1
 WHERE event_id=(SELECT event_id FROM pangolin_events.outbox WHERE (state='pending' AND next_attempt_at<=now()) OR (state='processing' AND locked_until<=now()) ORDER BY created_at,event_id FOR UPDATE SKIP LOCKED LIMIT 1)
 RETURNING envelope,attempts`, token, worker, p.Lease.Microseconds()).Scan(&raw, &attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, tx.Commit(ctx)
	}
	if err != nil {
		return nil, err
	}
	e, err := distributed.Decode(raw)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &outbox.Claim{Envelope: e, Token: token, Attempts: attempt}, nil
}
func (s *Store) Complete(ctx context.Context, c outbox.Claim, success bool, delay time.Duration, code string) error {
	state := "pending"
	if success {
		state = "sent"
	} else if c.Attempts >= s.policy.MaxAttempts {
		state = "dead"
	}
	tag, err := s.pool.Exec(ctx, `UPDATE pangolin_events.outbox SET state=$3,last_error=$4,next_attempt_at=now()+$5*interval '1 microsecond',lease_token=NULL,locked_until=NULL,sent_at=CASE WHEN $3='sent' THEN now() ELSE NULL END
 WHERE event_id=$1 AND lease_token=$2 AND state='processing'`, c.Envelope.ID, c.Token, state, code, delay.Microseconds())
	if err == nil && tag.RowsAffected() != 1 {
		return outbox.ErrLeaseLost
	}
	return err
}
func (s *Store) Persist(ctx context.Context, group string, e distributed.Envelope, raw []byte, producer string) error {
	if group == "" || producer == "" || producer != e.Producer {
		return scope.ErrDenied
	}
	encoded, encodeErr := distributed.Encode(e)
	if encodeErr != nil || !bytes.Equal(encoded, raw) {
		return errors.Join(inbox.ErrContentConflict, encodeErr)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	hash := sha256.Sum256(raw)
	_, err = tx.Exec(ctx, `INSERT INTO pangolin_events.inbox(consumer_group,event_id,tenant_id,scope_kind,scope_id,envelope,fingerprint,verified_producer) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(consumer_group,event_id) DO NOTHING`, group, e.ID, e.TenantID, e.ScopeKind, e.ScopeID, raw, hash[:], producer)
	if err != nil {
		return err
	}
	var stored, storedHash []byte
	var trusted string
	err = tx.QueryRow(ctx, `SELECT envelope,fingerprint,verified_producer FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, group, e.ID).Scan(&stored, &storedHash, &trusted)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, stored) || !bytes.Equal(hash[:], storedHash) || producer != trusted {
		return inbox.ErrContentConflict
	}
	return tx.Commit(ctx)
}
func (s *Store) Candidate(ctx context.Context, group string) (*inbox.Candidate, error) {
	c := &inbox.Candidate{Group: group}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT event_id,envelope,verified_producer,version,fingerprint FROM pangolin_events.inbox WHERE consumer_group=$1 AND state='pending' AND next_attempt_at<=now() ORDER BY created_at,event_id LIMIT 1`, group).Scan(&c.ID, &raw, &c.Producer, &c.Version, &c.Fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Envelope, err = distributed.Decode(raw)
	return c, err
}
func (s *Store) Lock(ctx context.Context, c inbox.Candidate) (bool, error) {
	ex, err := persistence.Current(ctx, true)
	if err != nil {
		return false, err
	}
	e := c.Envelope
	var raw, hash []byte
	var producer string
	err = ex.QueryRow(ctx, `SELECT envelope,fingerprint,verified_producer FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2 AND tenant_id=$3 AND scope_kind=$4 AND scope_id=$5 AND state='pending' AND version=$6 AND next_attempt_at<=now() FOR UPDATE SKIP LOCKED`, c.Group, c.ID, e.TenantID, e.ScopeKind, e.ScopeID, c.Version).Scan(&raw, &hash, &producer)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	encoded, err := distributed.Encode(e)
	if err != nil {
		return false, err
	}
	actualHash := sha256.Sum256(raw)
	if !bytes.Equal(raw, encoded) || !bytes.Equal(hash, actualHash[:]) || !bytes.Equal(hash, c.Fingerprint) || producer != c.Producer {
		return false, inbox.ErrContentConflict
	}
	return true, nil
}
func (s *Store) Processed(ctx context.Context, c inbox.Candidate) error {
	ex, err := persistence.Current(ctx, true)
	if err != nil {
		return err
	}
	b, ok := scope.Current(ctx)
	if !ok || b.Ref != (scope.ScopeRef{TenantID: c.Envelope.TenantID, Kind: c.Envelope.ScopeKind, ID: c.Envelope.ScopeID}) {
		return scope.ErrDenied
	}
	tag, err := ex.Exec(ctx, `UPDATE pangolin_events.inbox SET state='processed',processed_at=now(),version=version+1 WHERE consumer_group=$1 AND event_id=$2 AND version=$3 AND state='pending' AND fingerprint=$4 AND tenant_id=$5 AND scope_kind=$6 AND scope_id=$7`, c.Group, c.ID, c.Version, c.Fingerprint, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID)
	if err == nil && tag.RowsAffected() != 1 {
		return inbox.ErrContentConflict
	}
	return err
}
func (s *Store) Retry(ctx context.Context, c inbox.Candidate, maxAttempts int, delay time.Duration) error {
	if maxAttempts <= 0 || delay < 0 || c.Group == "" || c.ID == "" {
		return inbox.ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	e := c.Envelope
	if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.scope_kind',$2,true),set_config('app.scope_id',$3,true)`, e.TenantID, e.ScopeKind, e.ScopeID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE pangolin_events.inbox SET attempts=attempts+1,state=CASE WHEN attempts+1 >= $4 THEN 'dead' ELSE 'pending' END,next_attempt_at=now()+$5*interval '1 microsecond',version=version+1 WHERE consumer_group=$1 AND event_id=$2 AND state='pending' AND version=$3 AND fingerprint=$6`, c.Group, c.ID, c.Version, maxAttempts, delay.Microseconds(), c.Fingerprint)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
