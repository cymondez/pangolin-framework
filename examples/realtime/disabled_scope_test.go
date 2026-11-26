package main

// A99/disabled-scope: an authoritative scope-state disable must affect a
// connected user immediately through the live provider state: the active
// subscription loses every delivery at the final admission point and any new
// subscribe is refused. The disable is issued through the controlled
// trusted-fixture operation; the production wiring is unchanged.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/modules/realtime"
	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/scope"
)

func acceptScope(t *testing.T, hub *realtime.Hub, id string, ref scope.ScopeRef) {
	t.Helper()
	err := hub.AcceptEvent(t.Context(), distributed.Envelope{
		Type: realtime.EventType, SchemaVersion: 1, Producer: demoProducer, ID: id,
		TenantID: ref.TenantID, ScopeKind: ref.Kind, ScopeID: ref.ID,
		Payload: json.RawMessage(`{"channel":"organization","data":21}`),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestA99DisabledScope(t *testing.T) {
	settings := demoSettings{profile: "organization-enabled", key: "m7-acceptance-fixture-key"}
	authority, err := newFixture(settings)
	if err != nil {
		t.Fatal(err)
	}
	hub, err := authority.hub(nil, &demoRoot{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	organization := scope.ScopeRef{TenantID: "alpha", Kind: contracts.OrganizationKind, ID: organizationOne}
	credential := identity.Credential{Kind: identity.BearerKind, Value: settings.key + ":alpha/alice"}
	subject, err := hub.Authenticate(t.Context(), credential)
	if err != nil {
		t.Fatal(err)
	}
	session, err := hub.Open(t.Context(), credential, subject)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	subscription, err := json.Marshal(map[string]string{"tenant": organization.TenantID, "kind": organization.Kind, "id": organization.ID, "channel": "organization"})
	if err != nil {
		t.Fatal(err)
	}
	// The live member receives while the authoritative scope state is active.
	if _, err := session.Invoke(session.Context(), "Subscribe", []json.RawMessage{subscription}); err != nil {
		t.Fatal(err)
	}
	acceptScope(t, hub, "before-disable", organization)
	select {
	case item := <-session.Pending():
		written := ""
		err := session.WriteDelivery(item, func(_ context.Context, _ string, notification realtime.Notification) error {
			written = notification.ID
			return nil
		})
		if written != "before-disable" || err != nil {
			t.Fatalf("member delivery failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("member delivery missing before the disable")
	}
	// The controlled operation withdraws the scope state at the authoritative
	// side; nothing in the session or the subscription registry is touched.
	if err := authority.DisableScope(organization); err != nil {
		t.Fatal(err)
	}
	// A queued item for the now disabled scope is denied at the final
	// admission re-verification and the subscription is withdrawn.
	acceptScope(t, hub, "after-disable", organization)
	select {
	case item := <-session.Pending():
		err := session.WriteDelivery(item, func(context.Context, string, realtime.Notification) error {
			t.Fatal("disabled scope still delivered")
			return nil
		})
		if !errors.Is(err, scope.ErrDenied) {
			t.Fatalf("disabled scope admission: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delivery item missing after the disable")
	}
	// The live provider state refuses further subscriptions of the disabled scope.
	if _, err := session.Invoke(session.Context(), "Subscribe", []json.RawMessage{subscription}); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("subscribe after disable: %v", err)
	}
	// The withdrawn subscription no longer enqueues anything.
	acceptScope(t, hub, "post-withdrawal", organization)
	select {
	case item := <-session.Pending():
		err := session.WriteDelivery(item, func(_ context.Context, _ string, notification realtime.Notification) error {
			t.Fatalf("withdrawn subscription still queued %q", notification.ID)
			return nil
		})
		if err == nil {
			t.Fatal("withdrawn subscription delivered")
		}
	case <-time.After(600 * time.Millisecond):
	}
}
