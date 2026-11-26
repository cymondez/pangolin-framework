package realtime

// Named M7 contract scenarios (RT-001–RT-009, A96–A104), see spec §13.
// Scenarios that require the official @microsoft/signalr client, real PG/NATS
// services or multi-process deployment are mapped to
// examples/realtime/client/integration.test.mjs in tests/contract/m7.json and
// are not duplicated here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/scope"
)

func noDelivery(t *testing.T, s *Session) {
	t.Helper()
	select {
	case item := <-s.Pending():
		t.Fatalf("unexpected delivery %v", item.event)
	default:
	}
}

func publishRaw(t *testing.T, hub *Hub, userID, id, scopeID string, tenant string) {
	t.Helper()
	err := hub.AcceptEvent(t.Context(), distributed.Envelope{
		Type: EventType, SchemaVersion: 1, Producer: "test-realtime", ID: id,
		TenantID: tenant, ScopeKind: "tenant", ScopeID: scopeID,
		Payload: json.RawMessage(fmt.Sprintf(`{"channel":"updates","user_id":%q,"data":42}`, userID)),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestA96RT001(t *testing.T) {
	t.Run("concurrent-consume", func(t *testing.T) {
		hub, _ := testHub(t)
		first := openSession(t, hub, "alpha")
		second := openSession(t, hub, "alpha")
		third := openSession(t, hub, "beta")
		for _, s := range []*Session{first, second} {
			if err := subscribe(t, s, "Subscribe", "alpha"); err != nil {
				t.Fatal(err)
			}
		}
		publishEvent(t, hub, "fanout")
		for _, s := range []*Session{first, second} {
			if item := getDelivery(t, s); item.event.ID != "fanout" {
				t.Fatalf("unexpected event %v", item.event)
			}
		}
		noDelivery(t, third)
	})
}

func TestA98RT003(t *testing.T) {
	t.Run("tenant-collision", func(t *testing.T) {
		hub, _ := testHub(t)
		alpha := openSession(t, hub, "alpha")
		beta := openSession(t, hub, "beta")
		if err := subscribe(t, alpha, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		if err := subscribe(t, beta, "Subscribe", "beta"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "alpha-only")
		if item := getDelivery(t, alpha); item.event.ID != "alpha-only" {
			t.Fatalf("unexpected event %v", item.event)
		}
		noDelivery(t, beta)
	})
	t.Run("scope-collision", func(t *testing.T) {
		// The same channel name is registered for every tenant; exact-owner
		// keys must keep tenants from consuming each other's deliveries.
		hub, _ := testHub(t)
		left := openSession(t, hub, "alpha")
		right := openSession(t, hub, "beta")
		if err := subscribe(t, left, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		if err := subscribe(t, right, "Subscribe", "beta"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "left")
		if item := getDelivery(t, left); item.event.ID != "left" {
			t.Fatalf("unexpected event %v", item.event)
		}
		noDelivery(t, right)
		err := hub.AcceptEvent(t.Context(), distributed.Envelope{
			Type: EventType, SchemaVersion: 1, Producer: "test-realtime", ID: "right",
			TenantID: "beta", ScopeKind: "tenant", ScopeID: "beta",
			Payload: json.RawMessage(`{"channel":"updates","user_id":"same-user","data":7}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		if item := getDelivery(t, right); item.event.ID != "right" {
			t.Fatalf("unexpected event %v", item.event)
		}
		noDelivery(t, left)
	})
	t.Run("user-devices", func(t *testing.T) {
		hub, _ := testHub(t)
		deviceA := openSession(t, hub, "alpha")
		deviceB := openSession(t, hub, "alpha")
		other := openSession(t, hub, "beta")
		for _, s := range []*Session{deviceA, deviceB} {
			if err := subscribe(t, s, "Subscribe", "alpha"); err != nil {
				t.Fatal(err)
			}
		}
		if err := subscribe(t, other, "Subscribe", "beta"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "both-devices")
		for _, s := range []*Session{deviceA, deviceB} {
			if item := getDelivery(t, s); item.event.ID != "both-devices" {
				t.Fatalf("unexpected event %v", item.event)
			}
		}
		noDelivery(t, other)
		// User filtering excludes principals that do not own the notification.
		publishRaw(t, hub, "other-user", "unrelated", "alpha", "alpha")
		noDelivery(t, deviceA)
		noDelivery(t, deviceB)
		noDelivery(t, other)
	})
	t.Run("unsubscribe", func(t *testing.T) {
		hub, _ := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		if err := subscribe(t, session, "Unsubscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "dropped")
		noDelivery(t, session)
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "resubscribed")
		if item := getDelivery(t, session); item.event.ID != "resubscribed" {
			t.Fatalf("unexpected event %v", item.event)
		}
	})
	t.Run("forged-owner", func(t *testing.T) {
		hub, _ := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "beta"); !errors.Is(err, scope.ErrDenied) {
			t.Fatalf("forged tenant subscription admitted: %v", err)
		}
		// Resubscribing the owned scope keeps working.
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cross-tenant-host-negative", func(t *testing.T) {
		hub, _ := testHub(t)
		alpha := openSession(t, hub, "alpha")
		beta := openSession(t, hub, "beta")
		if err := subscribe(t, alpha, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		if err := subscribe(t, beta, "Subscribe", "beta"); err != nil {
			t.Fatal(err)
		}
		// An alpha-subscribed session must not host beta deliveries.
		if _, err := alpha.Invoke(alpha.Context(), "Subscribe", []json.RawMessage{
			json.RawMessage(`{"tenant":"beta","kind":"org.organization","id":"o1","channel":"updates"}`),
		}); !errors.Is(err, scope.ErrDenied) {
			t.Fatalf("cross-owner kind subscription admitted: %v", err)
		}
		if _, err := alpha.Invoke(alpha.Context(), "UnknownTarget", nil); !errors.Is(err, ErrMethod) {
			t.Fatalf("unexpected method error: %v", err)
		}
		_, err := hub.Publish(alpha.Context(), Destination{
			Scope: tenantScope("beta"), Channel: "updates"}, json.RawMessage(`"x"`))
		if !errors.Is(err, uow.ErrRequired) {
			t.Fatalf("publish outside a unit of work must be rejected, got %v", err)
		}
		publishEvent(t, hub, "host-check")
		noDelivery(t, beta)
	})
}

func TestA99RT004(t *testing.T) {
	t.Run("invalid-credential", func(t *testing.T) {
		hub, a := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "queued")
		a.revoked.Store(true)
		if err := session.WriteDelivery(getDelivery(t, session),
			func(context.Context, string, Notification) error { t.Fatal("revoked credential wrote"); return nil }); !errors.Is(err, ErrCredential) {
			t.Fatalf("unexpected error %v", err)
		}
		if session.Context().Err() == nil {
			t.Fatal("invalid credential connection still open")
		}
	})
	t.Run("membership-revocation", func(t *testing.T) {
		hub, a := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		if err := subscribe(t, session, "Unsubscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		// Rejoining a revoked scope requires the subscribe permission; the
		// removal itself stays an idempotent owner-key deletion.
		a.denied.Store(true)
		if err := subscribe(t, session, "Subscribe", "alpha"); !errors.Is(err, scope.ErrDenied) {
			t.Fatalf("revoked membership must be denied, got %v", err)
		}
		a.denied.Store(false)
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("permission-deny", func(t *testing.T) {
		hub, a := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "admitted")
		a.denied.Store(true)
		if err := session.WriteDelivery(getDelivery(t, session),
			func(context.Context, string, Notification) error { t.Fatal("denied receive wrote"); return nil }); !errors.Is(err, scope.ErrDenied) {
			t.Fatalf("unexpected error %v", err)
		}
	})
	t.Run("disabled-account", func(t *testing.T) {
		hub, a := testHub(t)
		a.revoked.Store(true)
		credentials := identity.Credential{Kind: identity.BearerKind, Value: "alpha"}
		subject, err := hub.Authenticate(t.Context(), credentials)
		if err == nil {
			t.Fatal("disabled account authenticated")
		}
		if _, err := hub.Open(t.Context(), credentials, subject); err == nil {
			t.Fatal("disabled account opened a session")
		}
	})
	t.Run("authority-down", func(t *testing.T) {
		hub, a := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "offline-check")
		a.offline.Store(true)
		if err := session.WriteDelivery(getDelivery(t, session),
			func(context.Context, string, Notification) error { t.Fatal("offline authority wrote"); return nil }); !errors.Is(err, scope.ErrAuthorityUnavailable) {
			t.Fatalf("unexpected error %v", err)
		}
		a.offline.Store(false)
		// Recovery keeps the subscription; a later delivery can be written.
		publishEvent(t, hub, "after-recovery")
		if err := session.WriteDelivery(getDelivery(t, session),
			func(_ context.Context, target string, event Notification) error {
				if target != "Update" || event.ID != "after-recovery" {
					t.Fatalf("unexpected write: %s %v", target, event)
				}
				return nil
			}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("before-write", func(t *testing.T) {
		// Barrier: revoke after the event is queued, before final admission.
		hub, a := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "barrier")
		a.denied.Store(true)
		if err := session.WriteDelivery(getDelivery(t, session),
			func(context.Context, string, Notification) error { t.Fatal("revoked barrier wrote"); return nil }); !errors.Is(err, scope.ErrDenied) {
			t.Fatalf("unexpected error %v", err)
		}
		// The failed receive removed the stale subscription; no later event of
		// the same marker reaches this session and no further queue item is
		// admitted after re-authorization.
		a.denied.Store(false)
		publishEvent(t, hub, "post-barrier")
		noDelivery(t, session)
	})
}

type a100Authority struct {
	currentAuthority
	ownerCalls int
}

// TestA100RT005 covers RT-005 operation boundaries. Scenarios committed by the
// official client with a real transaction (no-commit-no-success) map to
// integration.test.mjs; commit-unknown needs a real PG failure injection and
// stays pending in tests/contract/m7.json.
func TestA100RT005(t *testing.T) {
	t.Run("exact-method-owner", func(t *testing.T) {
		forged := Method{Name: "Forge", Access: Access{Permission: "echo"},
			Select: func(tenant string, _ []json.RawMessage) (scope.ScopeRef, error) {
				return tenantScope("beta"), nil
			},
			Invoke: func(context.Context, []json.RawMessage) (any, error) {
				return nil, errors.New("forged handler must not run")
			},
		}
		hub, _ := testHub(t, forged)
		session := openSession(t, hub, "alpha")
		if _, err := session.Invoke(t.Context(), "Forge", nil); !errors.Is(err, scope.ErrDenied) {
			t.Fatalf("forged method owner admitted: %v", err)
		}
	})
	t.Run("no-long-uow", func(t *testing.T) {
		hub, _ := testHub(t, Method{Name: "Echo", Access: Access{Permission: "echo"},
			Invoke: func(context.Context, []json.RawMessage) (any, error) { return "echo", nil }})
		session := openSession(t, hub, "alpha")
		if _, err := session.Invoke(t.Context(), "Echo", nil); err != nil {
			t.Fatal(err)
		}
		// No long-lived unit of work: after the operation returns, publishing
		// still requires an explicitly authorized writable unit of work.
		if _, err := hub.Publish(t.Context(), Destination{
			Scope: tenantScope("alpha"), Channel: "updates"}, json.RawMessage(`"x"`)); !errors.Is(err, uow.ErrRequired) {
			t.Fatalf("publish outside a unit of work must be rejected, got %v", err)
		}
		// The work group must be free again: the hub drains immediately.
		if err := hub.Quiesce(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("operation-scope-close", func(t *testing.T) {
		bindings := make(chan scope.ScopeRef, 8)
		hub, _ := testHub(t, Method{Name: "Wait", Access: Access{Permission: "wait"},
			Invoke: func(ctx context.Context, _ []json.RawMessage) (any, error) {
				bound, ok := scope.Current(ctx)
				if !ok {
					return nil, scope.ErrDenied
				}
				bindings <- bound.Ref
				<-ctx.Done()
				return nil, ctx.Err()
			}})
		session := openSession(t, hub, "alpha")
		done := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				_, err := session.Invoke(session.Context(), "Wait", nil)
				done <- err
			}()
		}
		// Admissions keep the per-operation slot; each operation binds the same
		// exact owner through a fresh, independently resolved scope.
		for i := 0; i < 2; i++ {
			select {
			case ref := <-bindings:
				if ref != tenantScope("alpha") {
					t.Fatalf("unexpected operation owner %v", ref)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("operation did not enter")
			}
		}
		session.Close()
		for i := 0; i < 2; i++ {
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("operation did not close with its scope: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("operation did not finish")
			}
		}
		select {
		case ref := <-bindings:
			t.Fatalf("operation scope leaked a binding after close: %v", ref)
		default:
		}
	})
}

// TestA101RT006 covers RT-006 ingress validation. Outbox commit/rollback and
// dedicated-connection reconciliation run against real PG services inside
// integration.test.mjs and are mapped there; commit-unknown reconciliation is
// pending (requires a PG failure injection).
func TestA101RT006(t *testing.T) {
	t.Run("stable-envelope", func(t *testing.T) {
		hub, _ := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "stable")
		item := getDelivery(t, session)
		if item.event.ID != "stable" || string(item.event.Data) != "42" {
			t.Fatalf("envelope identity not preserved: %v", item.event)
		}
	})
	t.Run("wrong-producer", func(t *testing.T) {
		hub, _ := testHub(t)
		err := hub.AcceptEvent(t.Context(), distributed.Envelope{
			Type: EventType, SchemaVersion: 1, Producer: "other-producer", ID: "wrong",
			TenantID: "alpha", ScopeKind: "tenant", ScopeID: "alpha",
			Payload: json.RawMessage(`{"channel":"updates","data":42}`),
		})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("foreign producer accepted: %v", err)
		}
	})
	t.Run("wrong-owner", func(t *testing.T) {
		hub, _ := testHub(t)
		err := hub.AcceptEvent(t.Context(), distributed.Envelope{
			Type: EventType, SchemaVersion: 1, Producer: "test-realtime", ID: "wrong-owner",
			TenantID: "alpha", ScopeKind: "org.organization", ScopeID: "o1",
			Payload: json.RawMessage(`{"channel":"updates","data":42}`),
		})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("unregistered owner accepted: %v", err)
		}
		err = hub.AcceptEvent(t.Context(), distributed.Envelope{
			Type: EventType, SchemaVersion: 1, Producer: "test-realtime", ID: "wrong-kind",
			TenantID: "alpha", ScopeKind: "tenant", ScopeID: "alpha",
			Payload: json.RawMessage(`{"channel":"nonexistent","data":42}`),
		})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("unknown channel accepted: %v", err)
		}
	})
}

// TestA102RT007 covers RT-007 broker-level negatives that need no real broker.
// Two live processes map to integration.test.mjs; broker redelivery, node
// restart and no-client-ack need real NATS semantics and stay pending.
func TestA102RT007(t *testing.T) {
	t.Run("wrong-scope-negative", func(t *testing.T) {
		hub, _ := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		// An event owned by another exact owner must not reach this session,
		// and must not be acknowledged as delivered to any subscriber here.
		publishRaw(t, hub, "", "other-scope", "beta", "beta")
		noDelivery(t, session)
		publishRaw(t, hub, "same-user", "own-scope", "alpha", "alpha")
		if item := getDelivery(t, session); item.event.ID != "own-scope" {
			t.Fatalf("unexpected event %v", item.event)
		}
	})
}

// TestA103RT008 covers RT-008 capacity, drain and lifecycle negatives.
// Observability redaction is exercised by the shared observability suite and
// stays pending for this package (metrics carry no tenant payload values).
func TestA103RT008(t *testing.T) {
	t.Run("admission-capacity", func(t *testing.T) {
		hub, _ := testHub(t)
		for range 4 {
			openSession(t, hub, "alpha")
		}
		extra, err := hub.Open(t.Context(), identity.Credential{Kind: identity.BearerKind, Value: "alpha"},
			Subject{Tenant: "alpha", Principal: scope.PrincipalRef{Kind: identity.PrincipalUser, ID: "same-user"}})
		if !errors.Is(err, ErrCapacity) {
			t.Fatalf("over-limit session admitted: %v", err)
		}
		if extra != nil {
			extra.Close()
		}
		if hub.ConnectionLimit() != 4 {
			t.Fatalf("unexpected connection limit %d", hub.ConnectionLimit())
		}
	})
	t.Run("slow-client", func(t *testing.T) {
		hub, _ := testHub(t)
		slow := openSession(t, hub, "alpha")
		fast := openSession(t, hub, "alpha")
		for _, s := range []*Session{slow, fast} {
			if err := subscribe(t, s, "Subscribe", "alpha"); err != nil {
				t.Fatal(err)
			}
		}
		publishEvent(t, hub, "fill")
		blocked := getDelivery(t, slow)
		if err := blocked.recipient.WriteDelivery(blocked, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unexpected error %v", err)
		}
		// The fast client keeps making progress while the slow one is blocked.
		if err := fast.WriteDelivery(getDelivery(t, fast), func(context.Context, string, Notification) error {
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("queue-full", func(t *testing.T) {
		hub, _ := testHub(t)
		slow := openSession(t, hub, "alpha")
		if err := subscribe(t, slow, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		// QueueSize is 1: the second unacknowledged event must not block the
		// ingress, and must close only the slow session.
		publishEvent(t, hub, "first")
		publishEvent(t, hub, "second")
		if slow.Context().Err() == nil {
			t.Fatal("full queue did not close the slow session")
		}
		fast := openSession(t, hub, "alpha")
		if err := subscribe(t, fast, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "third")
		if err := fast.WriteDelivery(getDelivery(t, fast), func(context.Context, string, Notification) error {
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("write-timeout", func(t *testing.T) {
		hub, _ := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		publishEvent(t, hub, "timeout-write")
		err := session.WriteDelivery(getDelivery(t, session), func(_ context.Context, _ string, _ Notification) error {
			return context.DeadlineExceeded
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected write error %v", err)
		}
		// The failed write is not a credential or subscription failure.
		publishEvent(t, hub, "retry-write")
		if err := session.WriteDelivery(getDelivery(t, session), func(_ context.Context, target string, event Notification) error {
			if event.ID != "retry-write" {
				t.Fatalf("unexpected event %v", event)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("active-drain", func(t *testing.T) {
		hub, _ := testHub(t)
		session := openSession(t, hub, "alpha")
		if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
		if err := hub.Quiesce(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Invoke(session.Context(), "Subscribe", nil); err == nil {
			t.Fatalf("quiesced hub admitted an operation: %v", err)
		}
		if err := hub.Drain(t.Context()); err != nil {
			t.Fatal(err)
		}
		if session.Context().Err() == nil {
			t.Fatal("drain left the connection open")
		}
	})
	t.Run("close-once", func(t *testing.T) {
		hub, _ := testHub(t)
		session := openSession(t, hub, "alpha")
		session.Close()
		session.Close()
		hub.Cancel()
		hub.Cancel()
		if _, err := session.Invoke(session.Context(), "Echo", nil); err == nil {
			t.Fatal("closed session invoked")
		}
	})
}
