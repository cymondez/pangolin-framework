package infrastructure

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/examples/bookstore/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Replicator is the example's tenant-only M1 queue/read-model slice. General
// queue stores, capacity controls and the complete failure matrix arrive in M3.
type Replicator struct {
	Pool    *pgxpool.Pool
	Broker  distributed.BrokerPublisher
	Tenants map[string]bool
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func (r *Replicator) Dispatch(ctx context.Context) (bool, error) {
	tx, e := r.Pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer rollback(tx)
	var raw []byte
	var id string
	token := rand.Text()
	e = tx.QueryRow(ctx, `UPDATE pangolin.outbox SET lease_token=$1,lease_until=now()+interval '2 seconds',attempts=attempts+1
WHERE event_id=(SELECT event_id FROM pangolin.outbox WHERE state='pending' AND attempts<8 AND (lease_until IS NULL OR lease_until<now()) ORDER BY created_at,event_id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING event_id,envelope`, token).Scan(&id, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return false, e
	}
	event, e := distributed.Decode(raw)
	if e != nil {
		return false, e
	}
	if !r.allowed(event) {
		return false, fmt.Errorf("worker tenant grant denied")
	}
	send, cancel := context.WithTimeout(ctx, 5*time.Second)
	e = r.Broker.Send(send, event)
	cancel()
	if e != nil {
		return false, e
	}
	tag, e := r.Pool.Exec(ctx, "UPDATE pangolin.outbox SET state='sent',lease_token=NULL,lease_until=NULL WHERE event_id=$1 AND lease_token=$2 AND state='pending'", id, token)
	if e == nil && tag.RowsAffected() != 1 {
		e = fmt.Errorf("outbox lease lost")
	}
	return true, e
}
func (r *Replicator) allowed(e distributed.Envelope) bool {
	return r.Tenants[e.TenantID] && e.ScopeKind == "tenant" && e.ScopeID == e.TenantID && e.Type == "bookstore.book_created" && e.SchemaVersion == 1 && e.Producer == "bookstore"
}
func (r *Replicator) Receive(ctx context.Context, event distributed.Envelope) error {
	if !r.allowed(event) {
		return fmt.Errorf("worker tenant grant denied")
	}
	raw, e := distributed.Encode(event)
	if e != nil {
		return e
	}
	tx, e := r.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollback(tx)
	if _, e = tx.Exec(ctx, "INSERT INTO pangolin.inbox(consumer_group,event_id,tenant_id,envelope) VALUES ('bookstore', $1,$2,$3) ON CONFLICT DO NOTHING", event.ID, event.TenantID, raw); e != nil {
		return e
	}
	var stored []byte
	if e = tx.QueryRow(ctx, "SELECT envelope FROM pangolin.inbox WHERE consumer_group='bookstore' AND event_id=$1", event.ID).Scan(&stored); e != nil {
		return e
	}
	if !bytes.Equal(raw, stored) {
		return fmt.Errorf("INBOX_CONTENT_CONFLICT")
	}
	return tx.Commit(ctx)
}
func (r *Replicator) Project(ctx context.Context) (bool, error) {
	tx, e := r.Pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer rollback(tx)
	var id string
	var raw []byte
	e = tx.QueryRow(ctx, "SELECT event_id,envelope FROM pangolin.inbox WHERE consumer_group='bookstore' AND NOT processed ORDER BY event_id FOR UPDATE SKIP LOCKED LIMIT 1").Scan(&id, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	event, e := distributed.Decode(raw)
	if e != nil {
		return false, e
	}
	if !r.allowed(event) {
		return false, fmt.Errorf("worker tenant grant denied")
	}
	var book domain.Book
	if e = json.Unmarshal(event.Payload, &book); e != nil {
		return false, e
	}
	if _, e = domain.NewBook(book.ID, book.Title); e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, "select set_config('pangolin.tenant_id',$1,true),set_config('app.tenant_id',$1,true),set_config('app.scope_kind',$2,true),set_config('app.scope_id',$3,true)", event.TenantID, event.ScopeKind, event.ScopeID); e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO pangolin.book_views(tenant_id,id,title,version) VALUES ($1,$2,$3,$4) ON CONFLICT (tenant_id,id) DO NOTHING", event.TenantID, book.ID, book.Title, book.Version); e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE pangolin.inbox SET processed=true WHERE consumer_group='bookstore' AND event_id=$1", id); e != nil {
		return false, e
	}
	return true, tx.Commit(ctx)
}
