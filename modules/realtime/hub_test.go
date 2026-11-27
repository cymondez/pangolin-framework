package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
)

type currentAuthority struct {
	offline, revoked, denied atomic.Bool
}

func (*currentAuthority) Kind() string { return identity.BearerKind }
func (a *currentAuthority) Verify(ctx context.Context, token identity.Credential) (identity.Result, error) {
	if a.revoked.Load() {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{Status: identity.StatusAuthenticated, Method: "test",
		Principal: scope.PrincipalRef{Kind: identity.PrincipalUser, ID: "same-user"}, Tenant: token.Value}, ctx.Err()
}

func (a *currentAuthority) Check(ctx context.Context, _ scope.PrincipalRef, _ string) (string, error) {
	if a.offline.Load() {
		return "", scope.ErrAuthorityUnavailable
	}
	return "current", ctx.Err()
}

func (a *currentAuthority) permission(ctx context.Context, _ scope.ScopeBinding, operation string) error {
	if a.denied.Load() && (operation == "subscribe" || operation == "receive") {
		return scope.ErrDenied
	}
	return ctx.Err()
}

func testHub(t *testing.T, methods ...Method) (*Hub, *currentAuthority) {
	t.Helper()
	a := &currentAuthority{}
	auth, err := identity.NewPipeline(a)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := tenancy.NewResolver(identity.Identity, a)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: a, Permission: a.permission})
	if err != nil {
		t.Fatal(err)
	}
	hub, err := New(Options{
		Producer: "test-realtime", Connect: Access{Permission: "connect"}, Methods: methods,
		Channels: []Channel{{Name: "updates", Target: "Update", Kinds: []string{"tenant"},
			Publish: Access{Permission: "publish"}, Subscribe: Access{Permission: "subscribe"}, Receive: Access{Permission: "receive"}}},
		MaxConnections: 4, MaxSubscriptions: 2, QueueSize: 1, MaxPayload: 1024, MaxOperations: 2,
		OperationTimeout: time.Second,
	}, auth, resolver, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	return hub, a
}

func openSession(t *testing.T, hub *Hub, tenant string) *Session {
	t.Helper()
	credentials := identity.Credential{Kind: identity.BearerKind, Value: tenant}
	subject, err := hub.Authenticate(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	session, err := hub.Open(t.Context(), credentials, subject)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return session
}

func subscribe(t *testing.T, s *Session, action, tenant string) error {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"tenant": tenant, "kind": "tenant", "id": tenant, "channel": "updates"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Invoke(s.Context(), action, []json.RawMessage{raw})
	return err
}

func publishEvent(t *testing.T, hub *Hub, id string) {
	t.Helper()
	err := hub.AcceptEvent(t.Context(), distributed.Envelope{
		Type: EventType, SchemaVersion: 1, Producer: "test-realtime", ID: id, TenantID: "alpha", ScopeKind: "tenant", ScopeID: "alpha",
		Payload: json.RawMessage(`{"channel":"updates","user_id":"same-user","data":42}`),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func getDelivery(t *testing.T, s *Session) Delivery {
	t.Helper()
	select {
	case item := <-s.Pending():
		return item
	default:
		t.Fatal("expected pending delivery")
		return Delivery{}
	}
}

func TestFinalDeliveryAdmission(t *testing.T) {
	hub, a := testHub(t)
	first := openSession(t, hub, "alpha")
	cross := openSession(t, hub, "beta")
	if err := subscribe(t, cross, "Subscribe", "alpha"); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if err := subscribe(t, cross, "Subscribe", "beta"); err != nil {
		t.Fatal(err)
	}
	if err := subscribe(t, first, "Subscribe", "alpha"); err != nil {
		t.Fatal(err)
	}

	publishEvent(t, hub, "old-subscription")
	item := getDelivery(t, first)
	select {
	case <-cross.Pending():
		t.Fatal("cross-tenant delivery")
	default:
	}
	if err := subscribe(t, first, "Unsubscribe", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := subscribe(t, first, "Subscribe", "alpha"); err != nil {
		t.Fatal(err)
	}
	writer := func(context.Context, string, Notification) error { t.Fatal("unauthorized write"); return nil }
	if err := first.WriteDelivery(item, writer); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}

	publishEvent(t, hub, "authority-offline")
	a.offline.Store(true)
	if err := first.WriteDelivery(getDelivery(t, first), writer); !errors.Is(err, scope.ErrAuthorityUnavailable) {
		t.Fatal(err)
	}
	a.offline.Store(false)

	publishEvent(t, hub, "after-deny")
	a.denied.Store(true)
	if err := first.WriteDelivery(getDelivery(t, first), writer); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	a.denied.Store(false)
	if err := subscribe(t, first, "Subscribe", "alpha"); err != nil {
		t.Fatal(err)
	}

	publishEvent(t, hub, "credential-expired")
	a.revoked.Store(true)
	if err := first.WriteDelivery(getDelivery(t, first), writer); !errors.Is(err, ErrCredential) {
		t.Fatal(err)
	}
	if first.Context().Err() == nil {
		t.Fatal("invalid credential connection still open")
	}
}

func TestSlowSessionAndCapacityIsolation(t *testing.T) {
	hub, _ := testHub(t)
	slow := openSession(t, hub, "alpha")
	fast := openSession(t, hub, "alpha")
	for _, s := range []*Session{slow, fast} {
		if err := subscribe(t, s, "Subscribe", "alpha"); err != nil {
			t.Fatal(err)
		}
	}
	writer := func(_ context.Context, target string, event Notification) error {
		if target != "Update" || string(event.Data) != "42" {
			t.Fatalf("unexpected message: %s %v", target, event)
		}
		return nil
	}
	publishEvent(t, hub, "first")
	if err := fast.WriteDelivery(getDelivery(t, fast), writer); err != nil {
		t.Fatal(err)
	}
	publishEvent(t, hub, "second")
	if slow.Context().Err() == nil {
		t.Fatal("full queue must close the slow session")
	}
	if err := fast.WriteDelivery(getDelivery(t, fast), writer); err != nil {
		t.Fatal(err)
	}
	if err := fast.WriteDelivery(getDelivery(t, slow), writer); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestOperationCancellationAndDrain(t *testing.T) {
	entered := make(chan struct{})
	hub, _ := testHub(t, Method{
		Name: "Wait", Access: Access{Permission: "wait"},
		Invoke: func(ctx context.Context, _ []json.RawMessage) (any, error) {
			if _, ok := scope.Current(ctx); !ok {
				return nil, scope.ErrDenied
			}
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	session := openSession(t, hub, "alpha")
	operation, cancel := context.WithCancel(session.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := session.Invoke(operation, "Wait", nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("operation did not enter")
	}
	if err := hub.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Invoke(session.Context(), "Wait", nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("operation did not cancel")
	}
	if err := hub.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if session.Context().Err() == nil {
		t.Fatal("drain did not close the connection")
	}
	if _, err := hub.Publish(t.Context(), Destination{Scope: tenantScope("alpha"), Channel: "updates"}, json.RawMessage("null")); !errors.Is(err, uow.ErrRequired) {
		t.Fatal(err)
	}
}
