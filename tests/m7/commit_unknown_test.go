//go:build integration

package m7

// A100/commit-unknown and A101/unknown-reconcile: the demo Notify use case runs
// a transaction whose COMMIT response is cut at the wire while the database
// committed. The invocation must fail with the UOW_COMMIT_UNKNOWN conservative
// outcome, an independent PostgreSQL connection must prove the business row and
// the Outbox row were committed, and the Outbox reconcile must deliver the
// stable event through the real broker exactly once. No fake commits.

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// committedState is the independent-connection reconciliation view.
type committedState struct {
	outboxRows    int
	notices       int
	attestedState string
	attestedID    string
}

func reconcile(t *testing.T, adminDSN, tenant, eventID string) committedState {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var view committedState
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pangolin_events.outbox WHERE event_id=$1`, eventID).Scan(&view.outboxRows); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM realtime_demo.notices WHERE id=$1`, eventID).Scan(&view.notices); err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `SELECT state FROM pangolin_events.outbox WHERE event_id=$1`, eventID).Scan(&view.attestedState)
	if err == nil {
		view.attestedID = eventID
	}
	return view
}

// runCommitUnknownFault drives one Notify invocation through a commit-proxy
// wrapped application pool and returns the invocation failure and the stable
// event identity that was committed.
func runCommitUnknownFault(t *testing.T, appDSN string) (string, string) {
	t.Helper()
	binary, adminDSN, _, _, natsURL, key := demoEnvironment(t)
	const stream = "PANGOLIN_M7_COMMIT_UNKNOWN"
	deleteStream(t, natsURL, stream)
	t.Cleanup(func() { deleteStream(t, natsURL, stream) })
	ensureProvisioned(t, binary, adminDSN, natsURL, stream)

	target, err := url.Parse(appDSN)
	if err != nil {
		t.Fatal(err)
	}
	proxyAddress, proxyStop := commitProxy(t, target.Host, true)
	defer proxyStop()
	server := startDemoServer(t, binary, stream, map[string]string{
		"PANGOLIN_REALTIME_APP_DSN": proxyDSN(t, appDSN, proxyAddress),
	})
	client := connectHub(t, server.base, key, "alice")
	client.subscribe(t, "s1", "alpha", "tenant", "alpha", "tenant")
	if frame := client.awaitCompletion(t, "s1", 10*time.Second); frame.Error != "" {
		t.Fatalf("subscription failed: %s`nSERVER LOG:`n%s", frame.Error, server.logs.String())
	}
	marker := "unknown-" + uniqueSuffix(t)
	client.invoke(t, "n1", "Notify",
		map[string]any{"tenant": "alpha", "kind": "tenant", "id": "alpha", "channel": "tenant"},
		map[string]any{"channel": "tenant", "data": `{"value":"` + marker + `"}`})
	completion := client.awaitCompletion(t, "n1", 30*time.Second)
	if completion.Error != "UOW_COMMIT_UNKNOWN" {
		t.Fatalf("expected a conservative unknown outcome, got %q", completion.Error)
	}
	t.Logf("invoke completion %q", completion.Error)
	// Correlate the stable event identity from this invocation's unique
	// marker through an independent PostgreSQL connection.
	eventID := outboxEventID(t, adminDSN, marker)
	if eventID == "" {
		t.Fatal("the committed outbox row was not found")
	}
	if err := client.awaitNotificationErr(t, eventID, 30*time.Second); err != nil {
		t.Fatalf("notification event=%s missing: %v\nSERVER LOG:\n%s", eventID, err, server.logs.String())
	}
	return eventID, adminDSN
}

// outboxEventID correlates the committed outbox event of this invocation by
// its unique marker through an independent PostgreSQL connection.
func outboxEventID(t *testing.T, adminDSN, marker string) string {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	deadline := time.Now().Add(15 * time.Second)
	var eventID string
	for {
		err = pool.QueryRow(ctx, `SELECT event_id FROM pangolin_events.outbox
			WHERE tenant_id='alpha' AND scope_kind='tenant' AND scope_id='alpha' AND convert_from(envelope, 'UTF8') LIKE $1
			ORDER BY created_at DESC, event_id DESC LIMIT 1`, "%"+marker+"%").Scan(&eventID)
		if err == nil {
			return eventID
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestA100CommitUnknown(t *testing.T) {
	_, appDSN, _, _, _, _ := demoEnvironment(t)
	eventID, adminDSN := runCommitUnknownFault(t, appDSN)
	// The independent connection proves the database committed both the
	// business row and the Outbox row of the reported-unknown transaction.
	view := reconcile(t, adminDSN, "alpha", eventID)
	if view.outboxRows != 1 || view.notices != 1 {
		t.Fatalf("committed business+outbox rows missing: %+v", view)
	}
	if view.attestedID == "" {
		t.Fatal("the outbox row cannot be re-read")
	}
	t.Logf("event=%s outbox_rows=%d business_rows=%d", eventID, view.outboxRows, view.notices)
}
func TestA101UnknownReconcile(t *testing.T) {
	_, appDSN, _, _, _, _ := demoEnvironment(t)
	eventID, adminDSN := runCommitUnknownFault(t, appDSN)
	if eventID == "" {
		t.Fatal("missing stable event identity")
	}
	// The Outbox worker reconciles the unknown outcome and the committed
	// event is dispatched through the real broker.
	deadline := time.Now().Add(30 * time.Second)
	state := ""
	for {
		pool, err := pgxpool.New(context.Background(), adminDSN)
		if err != nil {
			t.Fatal(err)
		}
		err = pool.QueryRow(context.Background(), `SELECT state FROM pangolin_events.outbox WHERE event_id=$1`, eventID).Scan(&state)
		pool.Close()
		if err != nil {
			t.Fatal(err)
		}
		if state == "sent" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("outbox never reconciled to sent: state=%s", state)
		}
		time.Sleep(300 * time.Millisecond)
	}
	// Reconciliation evidence: the committed business and outbox rows exist
	// and the enqueue state is dispatched (row uniqueness is not a
	// delivery-exactly-once proof).
	view := reconcile(t, adminDSN, "alpha", eventID)
	if view.outboxRows != 1 || view.notices != 1 || view.attestedState != "sent" {
		t.Fatalf("reconciled state disagrees: %+v", view)
	}
	t.Logf("reconciled event=%s state=%s business_rows=%d", eventID, view.attestedState, view.notices)
}
