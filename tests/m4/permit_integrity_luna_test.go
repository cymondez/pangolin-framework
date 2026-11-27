//go:build integration

package m4

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/events/outbox"
	eventpg "github.com/pangolin-framework/pangolin/events/postgres"
	persistence "github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	pjobs "github.com/pangolin-framework/pangolin/platform/jobs"
	jobpg "github.com/pangolin-framework/pangolin/platform/jobs/postgres"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5/pgconn"
)

func lunaPGCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected PostgreSQL error")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected PostgreSQL error, got %T: %v", err, err)
	}
	return pgErr.Code
}

func lunaJobPermitState(t *testing.T, f *fixture, slot int) (string, string) {
	t.Helper()
	var state, id string
	must(t, f.admin.QueryRow(context.Background(), `SELECT state,COALESCE(job_id,'') FROM pangolin_jobs.jobs_permits WHERE slot=$1`, slot).Scan(&state, &id))
	return state, id
}

func lunaJobPermitCounts(t *testing.T, f *fixture) (int, int, int) {
	t.Helper()
	var capacity, slots, assigned int
	must(t, f.admin.QueryRow(context.Background(), `SELECT capacity FROM pangolin_jobs.queue_capacity WHERE queue='jobs'`).Scan(&capacity))
	must(t, f.admin.QueryRow(context.Background(), `SELECT count(*),count(*) FILTER(WHERE state='assigned') FROM pangolin_jobs.jobs_permits`).Scan(&slots, &assigned))
	return capacity, slots, assigned
}

func lunaAssertJobsOneToOne(t *testing.T, f *fixture) {
	t.Helper()
	var bad int
	must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_jobs.jobs_permits p LEFT JOIN pangolin_jobs.jobs j ON j.permit_slot=p.slot AND j.id=p.job_id WHERE p.state='assigned' AND j.id IS NULL`).Scan(&bad))
	if bad != 0 {
		t.Fatalf("%d assigned permits do not point to their exact live job", bad)
	}
	must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM (SELECT permit_slot FROM pangolin_jobs.jobs WHERE permit_slot IS NOT NULL GROUP BY permit_slot HAVING count(*)<>1) d`).Scan(&bad))
	if bad != 0 {
		t.Fatalf("%d permit slots are not unique across live jobs", bad)
	}
}

func lunaEventPermitOwner(t *testing.T, f *fixture, queue string, slot int) (string, string) {
	t.Helper()
	var group, id string
	switch queue {
	case "outbox":
		must(t, f.admin.QueryRow(context.Background(), `SELECT COALESCE(event_id,'') FROM pangolin_events.outbox_permits WHERE slot=$1`, slot).Scan(&id))
	case "inbox":
		must(t, f.admin.QueryRow(context.Background(), `SELECT COALESCE(consumer_group,''),COALESCE(event_id,'') FROM pangolin_events.inbox_permits WHERE slot=$1`, slot).Scan(&group, &id))
	default:
		t.Fatal("unknown event queue")
	}
	return group, id
}

func lunaAssertEventBindings(t *testing.T, f *fixture, queue string) {
	t.Helper()
	var bad int
	switch queue {
	case "outbox":
		must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.outbox_permits p LEFT JOIN pangolin_events.outbox q ON q.permit_slot=p.slot AND q.event_id=p.event_id WHERE p.state='assigned' AND q.event_id IS NULL`).Scan(&bad))
		if bad == 0 {
			must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM (SELECT permit_slot FROM pangolin_events.outbox WHERE permit_slot IS NOT NULL GROUP BY permit_slot HAVING count(*)<>1) d`).Scan(&bad))
		}
	case "inbox":
		must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.inbox_permits p LEFT JOIN pangolin_events.inbox q ON q.permit_slot=p.slot AND q.consumer_group=p.consumer_group AND q.event_id=p.event_id WHERE p.state='assigned' AND q.event_id IS NULL`).Scan(&bad))
		if bad == 0 {
			must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM (SELECT permit_slot FROM pangolin_events.inbox WHERE permit_slot IS NOT NULL GROUP BY permit_slot HAVING count(*)<>1) d`).Scan(&bad))
		}
	default:
		t.Fatal("unknown event queue")
	}
	if bad != 0 {
		t.Fatalf("%d %s permit bindings are duplicated or not exact", bad, queue)
	}
}

func TestPermitIntegrityLunaEventSpoofAndRelease(t *testing.T) {
	f := newFixture(t, nil)
	p := outbox.Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 2}
	store, err := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{Outbox: 3, Inbox: 3})
	must(t, err)
	provisionQueue(t, f, "outbox", 3, "resize")
	provisionQueue(t, f, "inbox", 3, "resize")
	ref := scopeRefLuna(f.tenant)
	for i := 0; i < 2; i++ {
		e := envelope(ref)
		e.ID = fmt.Sprintf("luna-inbox-event-%d", i)
		raw, encodeErr := distributed.Encode(e)
		must(t, encodeErr)
		must(t, store.Persist(context.Background(), "luna-event-group", e, raw, e.Producer))
	}
	manager, publisher := eventManager(t, f, store)
	bound := f.bind(t, context.Background(), f.tenant, scope.PrincipalRef{Kind: "service", ID: "luna-producer"})
	var outboxIDs []string
	for i := 0; i < 2; i++ {
		e := envelope(ref)
		e.ID = fmt.Sprintf("luna-outbox-event-%d", i)
		outboxIDs = append(outboxIDs, e.ID)
		must(t, manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, e) }))
	}
	var inboxSlot, inboxOther, outboxSlot, outboxOther int
	must(t, f.admin.QueryRow(context.Background(), `SELECT permit_slot FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, "luna-event-group", "luna-inbox-event-0").Scan(&inboxSlot))
	must(t, f.admin.QueryRow(context.Background(), `SELECT permit_slot FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, "luna-event-group", "luna-inbox-event-1").Scan(&inboxOther))
	must(t, f.admin.QueryRow(context.Background(), `SELECT permit_slot FROM pangolin_events.outbox WHERE event_id=$1`, outboxIDs[0]).Scan(&outboxSlot))
	must(t, f.admin.QueryRow(context.Background(), `SELECT permit_slot FROM pangolin_events.outbox WHERE event_id=$1`, outboxIDs[1]).Scan(&outboxOther))
	if inboxSlot == inboxOther || outboxSlot == outboxOther {
		t.Fatal("two event rows share one permit")
	}
	lunaAssertEventBindings(t, f, "inbox")
	lunaAssertEventBindings(t, f, "outbox")

	// Worker INSERT privilege must not permit a caller-supplied slot.
	e := envelope(ref)
	e.ID = "luna-spoof-inbox"
	raw, err := distributed.Encode(e)
	must(t, err)
	hash := sha256.Sum256(raw)
	_, err = f.queue.Exec(context.Background(), `INSERT INTO pangolin_events.inbox(consumer_group,event_id,tenant_id,scope_kind,scope_id,envelope,fingerprint,verified_producer,permit_slot)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, "luna-event-group", e.ID, ref.TenantID, ref.Kind, ref.ID, raw, hash[:], e.Producer, inboxSlot)
	if err == nil {
		t.Fatal("event INSERT trusted a supplied permit_slot")
	}
	_ = lunaPGCode(t, err)

	// The worker role has UPDATE on inbox/outbox; changing ownership or slot is still forbidden.
	workerUpdate := func(query string, args ...any) error {
		tx, err := f.queue.Begin(context.Background())
		must(t, err)
		_, err = tx.Exec(context.Background(), `SELECT set_config('app.tenant_id',$1,true),set_config('app.scope_kind',$2,true),set_config('app.scope_id',$3,true)`, ref.TenantID, ref.Kind, ref.ID)
		must(t, err)
		_, err = tx.Exec(context.Background(), query, args...)
		_ = tx.Rollback(context.Background())
		return err
	}
	inboxUpdateErr := workerUpdate(`UPDATE pangolin_events.inbox SET permit_slot=NULL WHERE consumer_group=$1 AND event_id=$2`, "luna-event-group", "luna-inbox-event-0")
	outboxUpdateErr := workerUpdate(`UPDATE pangolin_events.outbox SET permit_slot=NULL WHERE event_id=$1`, outboxIDs[0])
	if inboxUpdateErr == nil || outboxUpdateErr == nil {
		t.Fatalf("worker changed permit binding: inbox=%v outbox=%v", inboxUpdateErr, outboxUpdateErr)
	}
	_ = lunaPGCode(t, inboxUpdateErr)
	_ = lunaPGCode(t, outboxUpdateErr)

	// Deleting either owner must free only its exact permit and preserve its peer.
	_, err = f.queue.Exec(context.Background(), `DELETE FROM pangolin_events.outbox WHERE event_id=$1`, outboxIDs[0])
	must(t, err)
	// Marking processed runs under the owner's transaction-local GUCs exactly
	// like the runtime consumer and must commit, so retention may purge it.
	processTx, err := f.queue.Begin(context.Background())
	must(t, err)
	defer processTx.Rollback(context.Background())
	_, err = processTx.Exec(context.Background(), `SELECT set_config('app.tenant_id',$1,true),set_config('app.scope_kind',$2,true),set_config('app.scope_id',$3,true)`, ref.TenantID, ref.Kind, ref.ID)
	must(t, err)
	processed, err := processTx.Exec(context.Background(), `UPDATE pangolin_events.inbox SET state='processed',processed_at=now() WHERE consumer_group=$1 AND event_id=$2`, "luna-event-group", "luna-inbox-event-0")
	must(t, err)
	if processed.RowsAffected() != 1 {
		t.Fatalf("processed inbox rows=%d, want 1", processed.RowsAffected())
	}
	must(t, processTx.Commit(context.Background()))
	purged, err := f.queue.Exec(context.Background(), `DELETE FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, "luna-event-group", "luna-inbox-event-0")
	must(t, err)
	if purged.RowsAffected() != 1 {
		t.Fatalf("retention deleted inbox rows=%d, want 1", purged.RowsAffected())
	}
	_, outOwner := lunaEventPermitOwner(t, f, "outbox", outboxOther)
	if _, deletedOwner := lunaEventPermitOwner(t, f, "outbox", outboxSlot); deletedOwner != "" {
		t.Fatalf("deleted outbox owner still assigned to slot: %q", deletedOwner)
	}
	if outOwner != outboxIDs[1] {
		t.Fatalf("outbox peer lost permit owner %q, want %q", outOwner, outboxIDs[1])
	}
	inOwnerGroup, inOwner := lunaEventPermitOwner(t, f, "inbox", inboxOther)
	if _, deletedOwner := lunaEventPermitOwner(t, f, "inbox", inboxSlot); deletedOwner != "" {
		t.Fatalf("deleted inbox owner still assigned to slot: %q", deletedOwner)
	}
	if inOwnerGroup != "luna-event-group" || inOwner != "luna-inbox-event-1" {
		t.Fatalf("inbox peer lost permit owner %q/%q", inOwnerGroup, inOwner)
	}
	lunaAssertEventBindings(t, f, "inbox")
	lunaAssertEventBindings(t, f, "outbox")
}

func TestPermitIntegrityLunaSpoofAndOwnerRelease(t *testing.T) {
	f := newFixture(t, nil)
	first, second := f.job(t), f.job(t)
	second.ID = second.ID + "-second"
	f.enqueue(t, first)
	f.enqueue(t, second)
	var firstSlot, secondSlot int
	must(t, f.admin.QueryRow(context.Background(), `SELECT permit_slot FROM pangolin_jobs.jobs WHERE id=$1`, first.ID).Scan(&firstSlot))
	must(t, f.admin.QueryRow(context.Background(), `SELECT permit_slot FROM pangolin_jobs.jobs WHERE id=$1`, second.ID).Scan(&secondSlot))
	if firstSlot == secondSlot {
		t.Fatalf("two live jobs share permit slot %d", firstSlot)
	}
	lunaAssertJobsOneToOne(t, f)

	ctx := f.bind(t, context.Background(), first.Owner.TenantID, first.Actor)
	err := f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
		ex, err := persistence.Current(work, true)
		if err != nil {
			return err
		}
		// Table INSERT privilege must not let a caller reuse an assigned slot.
		_, err = ex.Exec(work, `WITH cfg AS (SELECT set_config('pangolin.capacity_jobs','8',true),pangolin_jobs.check_capacity_config(8))
INSERT INTO pangolin_jobs.jobs(id,tenant_id,scope_kind,scope_id,document,fingerprint,due_at,deadline,permit_slot)
SELECT $1,tenant_id,scope_kind,scope_id,document,fingerprint,due_at,deadline,permit_slot FROM pangolin_jobs.jobs,cfg WHERE id=$2`, first.ID+"-spoof", first.ID)
		return err
	})
	if err == nil {
		t.Fatal("explicit permit_slot INSERT bypassed capacity claim")
	}
	if code := lunaPGCode(t, err); code == "" {
		t.Fatal("spoof INSERT did not fail with a PostgreSQL error")
	}

	err = f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
		ex, err := persistence.Current(work, true)
		if err != nil {
			return err
		}
		_, err = ex.Exec(work, `UPDATE pangolin_jobs.jobs SET permit_slot=NULL WHERE id=$1`, first.ID)
		return err
	})
	if err == nil {
		t.Fatal("UPDATE changed a queue row's permit_slot")
	}
	if code := lunaPGCode(t, err); code == "" {
		t.Fatal("spoof UPDATE did not fail with a PostgreSQL error")
	}
	lunaAssertJobsOneToOne(t, f)

	_, err = f.admin.Exec(context.Background(), `DELETE FROM pangolin_jobs.jobs WHERE id=$1`, first.ID)
	must(t, err)
	state, owner := lunaJobPermitState(t, f, firstSlot)
	if state != "free" || owner != "" {
		t.Fatalf("deleted job released wrong permit state=%q owner=%q", state, owner)
	}
	state, owner = lunaJobPermitState(t, f, secondSlot)
	if state != "assigned" || owner != second.ID {
		t.Fatalf("deleting one job disturbed other permit state=%q owner=%q want=%q", state, owner, second.ID)
	}
	lunaAssertJobsOneToOne(t, f)
}

func TestPermitIntegrityLunaHoleyShrinkGrow(t *testing.T) {
	f := newFixture(t, nil)
	provisionJobs(t, f, 6, "resize")
	f.policy.MaxPending = 6
	var err error
	f.store, err = jobpg.NewStore(f.queue, f.registry, f.policy, func(context.Context, string) error { return nil })
	must(t, err)
	jobs := make([]string, 5)
	for i := range jobs {
		j := f.job(t)
		j.ID = fmt.Sprintf("%s-hole-%d", j.ID, i)
		jobs[i] = j.ID
		f.enqueue(t, j)
	}
	for _, id := range []string{jobs[1], jobs[3]} {
		_, err = f.admin.Exec(context.Background(), `DELETE FROM pangolin_jobs.jobs WHERE id=$1`, id)
		must(t, err)
	}
	if err = provisionJobsErr(f, 3, "resize"); err != nil {
		t.Fatal(err)
	}
	if err = provisionJobsErr(f, 5, "resize"); err != nil {
		t.Fatal(err)
	}
	// Rebuild the store so its admission GUC matches the newly resized config.
	f.policy.MaxPending = 5
	f.store, err = jobpg.NewStore(f.queue, f.registry, f.policy, func(context.Context, string) error { return nil })
	must(t, err)
	capacity, slots, assigned := lunaJobPermitCounts(t, f)
	if capacity != 5 || slots != capacity || assigned != 3 {
		t.Fatalf("after holey resize capacity=%d slots=%d assigned=%d; want 5,5,3", capacity, slots, assigned)
	}
	lunaAssertJobsOneToOne(t, f)
	// Slot IDs are opaque identifiers, not quota values: growth may reuse a
	// raised max(slot) without compacting live bindings, so capacity is proven
	// behaviorally. The two remaining permits admit, a third is refused, counts
	// stay exact, and every admitted job owns exactly one permit.
	for i := 0; i < 2; i++ {
		j := f.job(t)
		j.ID = fmt.Sprintf("%s-fill-%d", j.ID, i)
		f.enqueue(t, j)
	}
	if c, s, a := lunaJobPermitCounts(t, f); c != 5 || s != 5 || a != 5 {
		t.Fatalf("after filling capacity=%d slots=%d assigned=%d; want 5,5,5", c, s, a)
	}
	over := f.job(t)
	over.ID = fmt.Sprintf("%s-over", over.ID)
	overCtx := f.bind(t, context.Background(), over.Owner.TenantID, over.Actor)
	err = f.manager.Within(overCtx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, over) })
	if !errors.Is(err, pjobs.ErrCapacity) {
		t.Fatalf("beyond-capacity admission err=%v, want jobs.ErrCapacity", err)
	}
	if c, s, a := lunaJobPermitCounts(t, f); c != 5 || s != 5 || a != 5 {
		t.Fatalf("refused admission changed counts capacity=%d slots=%d assigned=%d; want 5,5,5", c, s, a)
	}
	lunaAssertJobsOneToOne(t, f)
}

func TestPermitIntegrityLunaMissingAndWrongGUC(t *testing.T) {
	f := newFixture(t, nil)
	ctx := f.bind(t, context.Background(), "tenant-a", f.job(t).Actor)
	job := f.job(t)
	insert := `INSERT INTO pangolin_jobs.jobs(id,tenant_id,scope_kind,scope_id,document,fingerprint,due_at,deadline)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)`
	var missingID, wrongID string
	var err error
	for i, target := range []*string{&missingID, &wrongID} {
		j := job
		j.ID = fmt.Sprintf("%s-guc-%d", job.ID, i)
		*target = j.ID
		jsonRaw, marshalErr := json.Marshal(j)
		must(t, marshalErr)
		hash := sha256.Sum256(jsonRaw)
		statement := insert
		err = f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
			ex, err := persistence.Current(work, true)
			if err != nil {
				return err
			}
			if i == 1 {
				if _, err := ex.Exec(work, `SELECT set_config('pangolin.capacity_jobs','999',true)`); err != nil {
					return err
				}
			}
			_, err = ex.Exec(work, statement, j.ID, j.Owner.TenantID, j.Owner.Kind, j.Owner.ID, jsonRaw, hash[:], j.DueAt, j.Deadline)
			return err
		})
		if err == nil {
			value := "missing"
			if i == 1 {
				value = "mismatched"
			}
			t.Fatalf("%s capacity GUC inserted a row without matching provisioned config", value)
		}
	}
	var rows int
	must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_jobs.jobs WHERE id=ANY($1)`, []string{missingID, wrongID}).Scan(&rows))
	if rows != 0 {
		t.Fatalf("failed capacity GUC attempts left %d rows", rows)
	}
}

func TestPermitIntegrityLunaProvisionQueuesAtomicity(t *testing.T) {
	f := newFixture(t, nil)
	p := outbox.Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 2}
	store, err := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{Outbox: 2, Inbox: 2})
	must(t, err)
	provisionQueue(t, f, "outbox", 2, "resize")
	provisionQueue(t, f, "inbox", 2, "resize")
	for i := 0; i < 2; i++ {
		e := envelope(scopeRefLuna(f.tenant))
		e.ID = fmt.Sprintf("luna-inbox-%d", i)
		raw, encodeErr := distributed.Encode(e)
		must(t, encodeErr)
		must(t, store.Persist(context.Background(), "luna-group", e, raw, e.Producer))
	}
	db, err := sql.Open("pgx", f.admin.Config().ConnString())
	must(t, err)
	t.Cleanup(func() { _ = db.Close() })
	err = eventpg.ProvisionQueues(context.Background(), db, 5, 1, "resize")
	if err == nil {
		t.Fatal("second queue shrink below live rows unexpectedly succeeded")
	}
	var outboxCapacity, inboxCapacity int
	must(t, f.admin.QueryRow(context.Background(), `SELECT capacity FROM pangolin_events.queue_capacity WHERE queue='outbox'`).Scan(&outboxCapacity))
	must(t, f.admin.QueryRow(context.Background(), `SELECT capacity FROM pangolin_events.queue_capacity WHERE queue='inbox'`).Scan(&inboxCapacity))
	if outboxCapacity != 2 || inboxCapacity != 2 {
		t.Fatalf("partial ProvisionQueues mutation: outbox=%d inbox=%d, want 2/2", outboxCapacity, inboxCapacity)
	}
}

func scopeRefLuna(tenant string) scope.ScopeRef {
	return scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant}
}
