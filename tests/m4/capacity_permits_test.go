//go:build integration

package m4

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/events/inbox"
	"github.com/pangolin-framework/pangolin/events/outbox"
	eventpg "github.com/pangolin-framework/pangolin/events/postgres"
	persistence "github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/platform/jobs"
	jobpg "github.com/pangolin-framework/pangolin/platform/jobs/postgres"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5/pgconn"
)

// cause renders the full unwrapped chain so SQL details survive t.Fatalf.
func cause(err error) string {
	if err == nil {
		return "<nil>"
	}
	chain := []string{err.Error()}
	for e := errors.Unwrap(err); e != nil; e = errors.Unwrap(e) {
		chain = append(chain, e.Error())
	}
	return fmt.Sprintf("%d levels: %v", len(chain), chain)
}

func pgErrorCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("pg error required")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("not a pg error: %v", err)
	}
	return pgErr.Code
}

func provisionQueueErr(f *fixture, queue string, capacity int, action string) error {
	_, err := f.admin.Exec(context.Background(), `SELECT pangolin_events.provision_queue_permits($1,$2,$3)`, queue, capacity, action)
	return err
}
func provisionJobsErr(f *fixture, capacity int, action string) error {
	_, err := f.admin.Exec(context.Background(), `SELECT pangolin_jobs.provision_permits($1,$2)`, capacity, action)
	return err
}
func jobsAssigned(t *testing.T, f *fixture) int {
	t.Helper()
	var assigned int
	must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_jobs.jobs_permits WHERE state='assigned'`).Scan(&assigned))
	return assigned
}
func jobsRows(t *testing.T, f *fixture) int {
	t.Helper()
	var rows int
	must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_jobs.jobs`).Scan(&rows))
	return rows
}
func permitsAssigned(t *testing.T, f *fixture, table string) int {
	t.Helper()
	var assigned int
	must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.`+pgxIdentifier(table)+`_permits WHERE state='assigned'`).Scan(&assigned))
	return assigned
}
func queueRows(t *testing.T, f *fixture, table string) int {
	t.Helper()
	var rows int
	must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.`+pgxIdentifier(table)).Scan(&rows))
	return rows
}

// Test-only allowlist so query helpers cannot smuggle table names.
func pgxIdentifier(name string) string {
	switch name {
	case "outbox", "inbox", "outbox_permits", "inbox_permits", "queue_capacity":
		return name
	}
	panic("unknown table " + name)
}
func bindG(f *fixture, tenant string, p scope.PrincipalRef) (context.Context, error) {
	parent := context.Background()
	if _, err := f.admin.Exec(parent, `INSERT INTO pangolin_security.tenant_memberships VALUES($1,$2,$3,'active',1) ON CONFLICT DO NOTHING`, tenant, p.Kind, p.ID); err != nil {
		return nil, err
	}
	if _, err := f.admin.Exec(parent, `INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES($1,'tenant',$1) ON CONFLICT DO NOTHING`, tenant); err != nil {
		return nil, err
	}
	return f.resolve(parent, scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant}, p)
}

// The D5 core proof: one tenant's long UOW holds its own slot while other
// tenants enqueue and commit without waiting; the exact global cap refuses the
// next admission, rollback frees the reserved slot and purge releases permits
// of retained terminal rows.
func TestPermitJobsNoCrossTenantSerialization(t *testing.T) {
	f := newFixture(t, nil)
	provisionJobs(t, f, 4, "resize")
	f.policy.MaxPending = 4
	f.policy.Retention = time.Millisecond
	var err error
	f.store, err = jobpg.NewStore(f.queue, f.registry, f.policy, func(context.Context, string) error { return nil })
	must(t, err)
	actxA := f.bind(t, context.Background(), "tenant-a", scope.PrincipalRef{Kind: "user", ID: "alice"})
	actxB := f.bind(t, context.Background(), "tenant-b", scope.PrincipalRef{Kind: "user", ID: "alice"})
	base := f.job(t)
	base.DueAt = time.Unix(0, 0)
	base.Deadline = time.Now().Add(10 * time.Minute)

	enrolled := make(chan int, 1)
	release := make(chan struct{})
	done := make(chan error, 1)
	// Failure-safe barrier: any Fatal path still closes release exactly once
	// (deferred before the goroutine starts), so the blocked UOW never hangs.
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	go func() {
		done <- f.manager.Within(actxA, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := f.store.Enqueue(c, base); e != nil {
				return e
			}
			enrolled <- 1
			<-release
			return nil
		})
	}()
	select {
	case e := <-done:
		t.Fatalf("blocked UOW failed before enrollment: %s", cause(e))
	case <-enrolled:
	case <-time.After(10 * time.Second):
		var doneMsg string
		select {
		case e := <-done:
			doneMsg = cause(e)
		default:
		}
		t.Fatalf("timeout waiting for blocked UOW enrollment (done=%s)", doneMsg)
	}

	// A different tenant enqueues and commits while the first UOW is suspended;
	// under the previous global xact lock this admission had to wait for it.
	other := base
	other.ID = base.ID + "-b"
	other.Owner = scope.ScopeRef{TenantID: "tenant-b", Kind: "tenant", ID: "tenant-b"}
	began := time.Now()
	must(t, f.manager.Within(actxB, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, other) }))
	if elapsed := time.Since(began); elapsed > 2*time.Second {
		t.Fatalf("independent tenant admission waited %v on the blocked UOW", elapsed)
	}
	select {
	case e := <-done:
		t.Fatalf("blocked UOW finished early: %s", cause(e))
	default:
	}
	second := base
	second.ID = base.ID + "-c"
	must(t, f.manager.Within(actxA, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, second) }))
	fourth := base
	fourth.ID = base.ID + "-d"
	fourth.Owner = other.Owner
	must(t, f.manager.Within(actxB, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, fourth) }))
	fifth := base
	fifth.ID = base.ID + "-e"
	if e := f.manager.Within(actxA, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, fifth) }); !errors.Is(e, jobs.ErrCapacity) {
		t.Fatalf("hard global cap not enforced: %s (err=%v)", cause(e), e)
	}
	releaseOnce.Do(func() { close(release) })
	if e := <-done; e != nil {
		t.Fatalf("blocked UOW failed: %s", cause(e))
	}
	if rows := jobsRows(t, f); rows != 4 {
		t.Fatalf("rows=%d want 4", rows)
	}
	if assigned := jobsAssigned(t, f); assigned != 4 {
		t.Fatalf("assigned=%d want 4", assigned)
	}

	// Rollback must free the reserved slot without any repair step.
	if e := provisionJobsErr(f, 6, "resize"); e != nil {
		t.Fatal(e)
	}
	f.policy.MaxPending = 6
	f.store, err = jobpg.NewStore(f.queue, f.registry, f.policy, func(context.Context, string) error { return nil })
	must(t, err)
	doomed := base
	doomed.ID = base.ID + "-r"
	doomed.Owner = other.Owner
	e := f.manager.Within(actxB, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		if e := f.store.Enqueue(c, doomed); e != nil {
			return e
		}
		return errors.New("business failure")
	})
	if e == nil || errors.Is(e, jobs.ErrCapacity) {
		t.Fatalf("expected business rollback, got %s (err=%v)", cause(e), e)
	}
	if rows := jobsRows(t, f); rows != 4 {
		t.Fatalf("rollback leaked a row: %d", rows)
	}
	if assigned := jobsAssigned(t, f); assigned != 4 {
		t.Fatalf("rollback leaked a permit: %d", assigned)
	}
	reused := base
	reused.ID = base.ID + "-s"
	reused.Owner = other.Owner
	must(t, f.manager.Within(actxB, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, reused) }))

	// Retained terminal rows keep counting toward the cap; purge frees permits.
	claimed, err := f.store.Claim(context.Background(), "finalizer")
	must(t, err)
	must(t, f.store.Finish(context.Background(), *claimed, true, 0, ""))
	if assigned := jobsAssigned(t, f); assigned != 5 {
		t.Fatalf("terminal row lost its permit: %d", assigned)
	}
	time.Sleep(5 * time.Millisecond)
	removed, err := f.store.Purge(context.Background())
	must(t, err)
	if removed != 1 {
		t.Fatalf("purged %d", removed)
	}
	if assigned := jobsAssigned(t, f); assigned != 4 {
		t.Fatalf("purge did not release the permit: %d", assigned)
	}
	if rows := jobsRows(t, f); rows != 4 {
		t.Fatalf("rows=%d want 4", rows)
	}
}

// Upgrade path: existing rows are adopted by the transactional deployment
// backfill, install/resize guards fail closed, retained terminal rows keep
// their permits until purge, and duplicate keys need no free capacity.
func TestPermitEventUpgradeAdoption(t *testing.T) {
	f := newFixture(t, nil)
	p := outbox.Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 2}
	ref := scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"}
	seed := envelope(ref)
	unbounded, err := eventpg.NewStore(f.queue, p)
	must(t, err)
	for _, id := range []string{"seed-1", "seed-2"} {
		e := seed
		e.ID = id
		raw, encodeErr := distributed.Encode(e)
		must(t, encodeErr)
		must(t, unbounded.Persist(context.Background(), "m4-group", e, raw, e.Producer))
	}
	provisionQueue(t, f, "inbox", 2, "install")
	var slot int
	must(t, f.admin.QueryRow(context.Background(), `SELECT permit_slot FROM pangolin_events.inbox WHERE event_id='seed-1'`).Scan(&slot))
	if slot != 1 {
		t.Fatalf("seed-1 slot %d", slot)
	}
	must(t, f.admin.QueryRow(context.Background(), `SELECT permit_slot FROM pangolin_events.inbox WHERE event_id='seed-2'`).Scan(&slot))
	if slot != 2 {
		t.Fatalf("seed-2 slot %d", slot)
	}
	if assigned := permitsAssigned(t, f, "inbox"); assigned != 2 {
		t.Fatalf("adopted assigned=%d", assigned)
	}
	// Deployment guards fail closed with dedicated codes.
	if code := pgErrorCode(t, provisionQueueErr(f, "inbox", 2, "install")); code != "PC008" {
		t.Fatalf("re-install code %s", code)
	}
	if code := pgErrorCode(t, provisionQueueErr(f, "inbox", 1, "resize")); code != "PC009" {
		t.Fatalf("shrink-below-rows code %s", code)
	}

	store, err := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{Outbox: 2, Inbox: 2})
	must(t, err)
	provisionQueue(t, f, "outbox", 2, "resize")
	manager, publisher := eventManager(t, f, store)
	bound := f.bind(t, context.Background(), "tenant-a", scope.PrincipalRef{Kind: "service", ID: "producer"})
	for _, suffix := range []string{"-o1", "-o2"} {
		e := envelope(ref)
		e.ID = seed.ID + suffix
		must(t, manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, e) }))
	}
	full := envelope(ref)
	if e := manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, full) }); !errors.Is(e, eventpg.ErrCapacity) {
		t.Fatalf("outbox overcap not refused: %v", e)
	}
	// Terminal rows retain their permits: marking sent must not free capacity.
	_, err = f.admin.Exec(context.Background(), `UPDATE pangolin_events.outbox SET state='sent',sent_at=now()`)
	must(t, err)
	if e := manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, full) }); !errors.Is(e, eventpg.ErrCapacity) {
		t.Fatalf("sent rows must retain their permit: %v", e)
	}
	removed, err := store.Purge(context.Background(), eventpg.Retention{Broker: time.Millisecond, ManualReplay: time.Millisecond, Inbox: time.Millisecond, OutboxSent: time.Millisecond})
	must(t, err)
	if removed != 2 {
		t.Fatalf("purged %d", removed)
	}
	must(t, manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, full) }))
	if assigned := permitsAssigned(t, f, "outbox"); assigned != 1 {
		t.Fatalf("outbox assigned=%d", assigned)
	}
	if rows := queueRows(t, f, "outbox"); rows != 1 {
		t.Fatalf("outbox rows=%d", rows)
	}

	// A duplicate key at full capacity needs no permit; verified duplicates are
	// deduplicated, conflicting content is refused.
	// seed.ID is random; the seeded rows above use the fixed IDs 'seed-1' and
	// 'seed-2', so a true duplicate must target one of those IDs.
	seedCopy := seed
	seedCopy.ID = "seed-1"
	seedRaw, err := distributed.Encode(seedCopy)
	must(t, err)
	must(t, store.Persist(context.Background(), "m4-group", seedCopy, seedRaw, seedCopy.Producer))
	if assigned := permitsAssigned(t, f, "inbox"); assigned != 2 {
		t.Fatalf("duplicate consumed capacity: %d", assigned)
	}
	conflict := seed
	conflict.ID = "seed-1"
	conflict.Payload = json.RawMessage(`{"value":"altered-secret"}`)
	conflictRaw, err := distributed.Encode(conflict)
	must(t, err)
	if e := store.Persist(context.Background(), "m4-group", conflict, conflictRaw, conflict.Producer); !errors.Is(e, inbox.ErrContentConflict) {
		t.Fatalf("conflicting duplicate accepted: %v", e)
	}
}

// Concurrent duplicate inserts race the last slot without leaking permits. The
// start barrier does not force the racers to overlap: if the first commit lands
// before the others read, every racer is a verified duplicate (success=8,
// refused=0); if they do overlap, one wins and the rest are refused. Both
// schedules are acceptable as long as exactly one row and three assigned
// permits remain, and a distinct event at full capacity is hard-refused.
// A terminated backend releases its claim exactly like an explicit rollback.
func TestPermitDuplicateRacesAndCrashRecovery(t *testing.T) {
	f := newFixture(t, nil)
	p := outbox.Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 2}
	store, err := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{Outbox: 4, Inbox: 3})
	must(t, err)
	provisionQueue(t, f, "outbox", 4, "resize")
	provisionQueue(t, f, "inbox", 3, "resize")
	ref := scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"}
	raced := envelope(ref)
	first := envelope(ref)
	second := envelope(ref)
	for _, e := range []distributed.Envelope{first, second} {
		raw, encodeErr := distributed.Encode(e)
		must(t, encodeErr)
		must(t, store.Persist(context.Background(), "m4-group", e, raw, e.Producer))
	}
	racedRaw, err := distributed.Encode(raced)
	must(t, err)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var success, refused atomic.Int32
	errs := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			e := store.Persist(context.Background(), "m4-group", raced, racedRaw, raced.Producer)
			switch {
			case e == nil:
				success.Add(1)
			case errors.Is(e, eventpg.ErrCapacity):
				refused.Add(1)
			default:
				errs <- e
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if success.Load() < 1 || success.Load()+refused.Load() != 8 {
		t.Fatalf("race outcome success=%d refused=%d", success.Load(), refused.Load())
	}
	if rows := queueRows(t, f, "inbox"); rows != 3 {
		t.Fatalf("race rows=%d", rows)
	}
	if assigned := permitsAssigned(t, f, "inbox"); assigned != 3 {
		t.Fatalf("race leaked permits: %d", assigned)
	}
	var duplicates int
	must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.inbox WHERE event_id=$1`, raced.ID).Scan(&duplicates))
	if duplicates != 1 {
		t.Fatalf("duplicated row x%d", duplicates)
	}

	// No oversubscription: a distinct event once capacity is full is refused
	// regardless of how the duplicate race resolved.
	overflow := envelope(ref)
	overflowRaw, err := distributed.Encode(overflow)
	must(t, err)
	if e := store.Persist(context.Background(), "m4-group", overflow, overflowRaw, overflow.Producer); !errors.Is(e, eventpg.ErrCapacity) {
		t.Fatalf("distinct event at full capacity err=%v, want eventpg.ErrCapacity", e)
	}
	if rows := queueRows(t, f, "inbox"); rows != 3 {
		t.Fatalf("overflow changed rows=%d", rows)
	}
	if assigned := permitsAssigned(t, f, "inbox"); assigned != 3 {
		t.Fatalf("overflow changed assigned permits: %d", assigned)
	}

	// A terminated backend frees its claim exactly like an explicit rollback.
	f.store, err = jobpg.NewStore(f.queue, f.registry, f.policy, func(context.Context, string) error { return nil })
	must(t, err)
	job := f.job(t)
	job.DueAt = time.Now().Add(time.Minute)
	job.Deadline = job.DueAt.Add(10 * time.Minute)
	// Failure-safe barrier: always release the waiting UOW, even on Fatal.
	crash := make(chan struct{})
	var crashOnce sync.Once
	defer crashOnce.Do(func() { close(crash) })
	pidCh := make(chan int64, 1)
	crashDone := make(chan error, 1)
	go func() {
		jctx, bindErr := bindG(f, "tenant-a", job.Actor)
		if bindErr != nil {
			crashDone <- bindErr
			return
		}
		crashDone <- f.manager.Within(jctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := f.store.Enqueue(c, job); e != nil {
				return e
			}
			ex, e := persistence.Current(c, false)
			if e != nil {
				return e
			}
			var pid int64
			if e = ex.QueryRow(c, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				return e
			}
			pidCh <- pid
			<-crash
			return nil
		})
	}()
	select {
	case e := <-crashDone:
		t.Fatalf("crash UOW failed before reporting pid: %s", cause(e))
	case pid := <-pidCh:
		_, err = f.admin.Exec(context.Background(), `SELECT pg_terminate_backend($1)`, pid)
		must(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for crash UOW to report backend pid")
	}
	crashOnce.Do(func() { close(crash) })
	if e := <-crashDone; e == nil {
		t.Fatal("terminated transaction reported success")
	}
	if rows := jobsRows(t, f); rows != 0 {
		t.Fatalf("crash leaked the job row: %d", rows)
	}
	if assigned := jobsAssigned(t, f); assigned != 0 {
		t.Fatalf("crash leaked the permit: %d", assigned)
	}
	replacement := f.job(t)
	replacement.DueAt = time.Now().Add(time.Minute)
	replacement.Deadline = replacement.DueAt.Add(10 * time.Minute)
	jctx := f.bind(t, context.Background(), "tenant-a", replacement.Actor)
	must(t, f.manager.Within(jctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, replacement) }))
}

// Typed failures keep bounded/unbounded assembly explicit and RLS stays closed:
// the app role never gains access to other owners' rows or permit tables.
func TestPermitTypedFailuresAndRLS(t *testing.T) {
	f := newFixture(t, nil)
	p := outbox.Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 2}
	store, err := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{Outbox: 2, Inbox: 2})
	must(t, err)
	provisionQueue(t, f, "outbox", 2, "resize")
	manager, publisher := eventManager(t, f, store)
	bound := f.bind(t, context.Background(), "tenant-a", scope.PrincipalRef{Kind: "service", ID: "producer"})
	first := envelope(scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	must(t, manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, first) }))
	inbound := envelope(scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	inboundRaw, err := distributed.Encode(inbound)
	must(t, err)
	// Bounded caller on an unprovisioned inbox fails closed.
	if e := store.Persist(context.Background(), "m4-group", inbound, inboundRaw, inbound.Producer); !errors.Is(e, eventpg.ErrCapacityUnprovisioned) {
		t.Fatalf("unprovisioned inbox: %v", e)
	}
	provisionQueue(t, f, "inbox", 2, "resize")
	unbounded, err := eventpg.NewStore(f.queue, p)
	must(t, err)
	// Unbounded caller against a provisioned queue is refused explicitly.
	if e := unbounded.Persist(context.Background(), "m4-group", inbound, inboundRaw, inbound.Producer); !errors.Is(e, eventpg.ErrCapacityRequired) {
		t.Fatalf("unbounded on provisioned: %v", e)
	}
	// Caller/deployment capacity mismatch is a typed conflict verified with an
	// indexed config lookup on the admission statement.
	mismatch, err := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{Outbox: 5, Inbox: 5})
	must(t, err)
	if e := mismatch.Persist(context.Background(), "m4-group", inbound, inboundRaw, inbound.Producer); !errors.Is(e, eventpg.ErrCapacityConfigConflict) {
		t.Fatalf("capacity mismatch: %v", e)
	}

	// Jobs mirror the same contract after the permit pool is torn down.
	_, err = f.admin.Exec(context.Background(), `DELETE FROM pangolin_jobs.queue_capacity`)
	must(t, err)
	_, err = f.admin.Exec(context.Background(), `DELETE FROM pangolin_jobs.jobs_permits`)
	must(t, err)
	jobA := f.job(t)
	jobA.DueAt = time.Now().Add(time.Minute)
	jobA.Deadline = jobA.DueAt.Add(10 * time.Minute)
	jobCtx := f.bind(t, context.Background(), "tenant-a", jobA.Actor)
	// A legacy pre-upgrade row is seeded by the deployment role WITHOUT capacity
	// GUC while no config row exists; it becomes the adoption source. The
	// bounded caller is still rejected until provisioning installs the pool.
	legacyDoc, err := json.Marshal(jobA)
	must(t, err)
	legacyHash := sha256.Sum256(legacyDoc)
	_, err = f.admin.Exec(context.Background(), `INSERT INTO pangolin_jobs.jobs(id,tenant_id,scope_kind,scope_id,document,fingerprint,due_at,deadline) VALUES($1,$2,'tenant',$2,$3,$4,$5,$6)`, jobA.ID, jobA.Owner.TenantID, legacyDoc, legacyHash[:], jobA.DueAt, jobA.Deadline)
	must(t, err)
	if e := f.manager.Within(jobCtx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, f.job(t)) }); !errors.Is(e, jobpg.ErrCapacityUnprovisioned) {
		t.Fatalf("unprovisioned jobs with legacy row: %s (err=%v)", cause(e), e)
	}
	// Deployment provisioning adopts the legacy row transactionally; afterwards
	// the bounded caller admits and the legacy row keeps its own permit.
	provisionJobs(t, f, f.policy.MaxPending, "resize")
	var legacyJobID string
	must(t, f.admin.QueryRow(context.Background(), `SELECT job_id FROM pangolin_jobs.jobs_permits WHERE job_id=$1 AND state='assigned'`, jobA.ID).Scan(&legacyJobID))
	accepted := f.job(t)
	accepted.DueAt = time.Now().Add(time.Minute)
	accepted.Deadline = accepted.DueAt.Add(10 * time.Minute)
	must(t, f.manager.Within(jobCtx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, accepted) }))
	if code := pgErrorCode(t, provisionJobsErr(f, 1, "resize")); code != "JC009" {
		t.Fatalf("shrink below rows code %s", code)
	}

	// RLS: the app role sees only its own owner rows and never the permit or
	// config tables; cross-owner rows stay invisible.
	var visible int
	must(t, f.manager.Within(jobCtx, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error {
		ex, e := persistence.Current(c, false)
		if e != nil {
			return e
		}
		return ex.QueryRow(c, `SELECT count(*) FROM pangolin_jobs.jobs`).Scan(&visible)
	}))
	if visible != 2 {
		t.Fatalf("cross owner visibility %d", visible)
	}
	if e := f.app.QueryRow(context.Background(), `SELECT slot FROM pangolin_jobs.jobs_permits LIMIT 1`).Scan(nil); e == nil {
		t.Fatal("app role read permits")
	} else if code := pgErrorCode(t, e); code != "42501" {
		t.Fatalf("app permits code %s", code)
	}
	var permitRows int
	must(t, f.app.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_jobs.jobs WHERE tenant_id='tenant-b'`).Scan(&permitRows))
	if permitRows != 0 {
		t.Fatalf("cross owner visibility %d", permitRows)
	}
}
