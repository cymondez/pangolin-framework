package realtime

// Negative and boundary paths for hub, session and ingress admission that the
// contract scenarios do not exercise on their own. Assertions check the exact
// guard errors; none of them relax production behaviour.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
)

func demoChannel() Channel {
	return Channel{Name: "updates", Target: "Update", Kinds: []string{"tenant"},
		Publish: Access{Permission: "publish"}, Subscribe: Access{Permission: "subscribe"},
		Receive: Access{Permission: "receive"}}
}

func renamedChannel(name string) Channel {
	channel := demoChannel()
	channel.Name = name
	return channel
}

func echoMethod() Method {
	return Method{Name: "Echo", Access: Access{Permission: "echo"}, Invoke: func(_ context.Context, args []json.RawMessage) (any, error) {
		return len(args), nil
	}}
}

func stubPipeline() *identity.Pipeline {
	pipeline, _ := identity.NewPipeline(&currentAuthority{})
	return pipeline
}

func newAuthorityHub(authority *currentAuthority) (*Hub, error) {
	return newAuthorityOptionsHub(authority, Options{
		Producer: "test-realtime", Connect: Access{Permission: "connect"},
		Channels: []Channel{demoChannel()}, MaxConnections: 4, MaxSubscriptions: 2,
		QueueSize: 1, MaxPayload: 1024, MaxOperations: 2, OperationTimeout: time.Second,
	})
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	base := Options{
		Producer: "test-realtime", Connect: Access{Permission: "connect"},
		MaxConnections: 4, MaxSubscriptions: 2, QueueSize: 1, MaxPayload: 1024,
		MaxOperations: 2, OperationTimeout: time.Second,
	}
	reserved := Method{Name: "Subscribe", Access: Access{Permission: "x"}, Invoke: func(context.Context, []json.RawMessage) (any, error) {
		return nil, nil
	}}
	noInvoke := Method{Name: "Echo", Access: Access{Permission: "echo"}}
	unauthorizedInvoke := Method{Name: "Echo", Access: Access{}, Invoke: func(context.Context, []json.RawMessage) (any, error) {
		return nil, nil
	}}
	badChannel := demoChannel()
	badChannel.Target = ""
	emptyChannel := Channel{Name: "other", Kinds: []string{"tenant"}, Publish: Access{Permission: "publish"}}
	unregisteredKind := demoChannel()
	unregisteredKind.Kinds = []string{"vendor.internal"}
	cases := map[string]func(*Options){
		"missing-invoke":        func(o *Options) { o.Methods = []Method{noInvoke} },
		"unauthorized-invoke":   func(o *Options) { o.Methods = []Method{unauthorizedInvoke} },
		"reserved-method":       func(o *Options) { o.Methods = []Method{reserved} },
		"duplicate-method":      func(o *Options) { o.Methods = []Method{echoMethod(), echoMethod()} },
		"channel-missing-kind":  func(o *Options) { o.Channels = []Channel{emptyChannel} },
		"channel-missing-write": func(o *Options) { o.Channels = []Channel{badChannel} },
		"unregistered-kind":     func(o *Options) { o.Channels = []Channel{unregisteredKind} },
		"duplicate-channel":     func(o *Options) { o.Channels = []Channel{demoChannel(), demoChannel()} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			options := base
			mutate(&options)
			hub, err := New(options, stubPipeline(), nil, nil, nil)
			if hub != nil {
				hub.Cancel()
			}
			if err == nil {
				t.Fatal("invalid option was accepted")
			}
		})
	}
}

type servicePrincipalAuthority struct{ currentAuthority }

func (a *servicePrincipalAuthority) Verify(ctx context.Context, token identity.Credential) (identity.Result, error) {
	result, err := a.currentAuthority.Verify(ctx, token)
	result.Principal.Kind = identity.PrincipalService
	return result, err
}

func TestAuthenticateRejectsNonUserPrincipal(t *testing.T) {
	authority := &servicePrincipalAuthority{}
	hub, err := newServicePrincipalHub(authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	if _, err := hub.Authenticate(context.Background(),
		identity.Credential{Kind: identity.BearerKind, Value: "alpha"}); !errors.Is(err, ErrCredential) {
		t.Fatalf("non-user principal authenticated: %v", err)
	}
}

func TestOpenRejectsShutdownStates(t *testing.T) {
	hub, _ := newModuleHub("test-realtime", nil)
	t.Cleanup(hub.Cancel)
	credential := identity.Credential{Kind: identity.BearerKind, Value: "alpha"}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := hub.Open(cancelled, credential, Subject{}); !errors.Is(err, context.Canceled) && !errors.Is(err, ErrCredential) {
		t.Fatalf("cancelled open admitted: %v", err)
	}
	subject, err := hub.Authenticate(context.Background(), credential)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Quiesce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Open(context.Background(), credential, subject); !errors.Is(err, ErrClosed) {
		t.Fatalf("quiesced hub admitted a connection: %v", err)
	}
}

type servicePrincipalHubSpec struct{}

func newServicePrincipalHub(authority *servicePrincipalAuthority) (*Hub, error) {
	pipeline, err := identity.NewPipeline(authority)
	if err != nil {
		return nil, err
	}
	resolver, err := tenancy.NewResolver(identity.Identity, authority)
	if err != nil {
		return nil, err
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: authority, Permission: authority.permission})
	if err != nil {
		return nil, err
	}
	return New(Options{
		Producer: "test-realtime", Connect: Access{Permission: "connect"},
		Channels: []Channel{demoChannel()}, MaxConnections: 4, MaxSubscriptions: 2,
		QueueSize: 1, MaxPayload: 1024, MaxOperations: 2, OperationTimeout: time.Second,
	}, pipeline, resolver, registry, nil)
}

func TestInvokeAndOperationGuards(t *testing.T) {
	authority := &currentAuthority{}
	hub, err := newAuthorityHub(authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	session := openSession(t, hub, "alpha")
	if _, err := session.Invoke(nil, "Echo", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil context invocation admitted: %v", err)
	}
	cancelled, cancel := context.WithCancel(session.Context())
	cancel()
	if _, err := session.Invoke(cancelled, "Echo", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled invocation admitted: %v", err)
	}
	// A revoked credential closes the session at verify time.
	authority.revoked.Store(true)
	if _, err := session.Invoke(session.Context(), "Echo", nil); !errors.Is(err, ErrCredential) {
		t.Fatalf("revoked invocation error: %v", err)
	}
	if session.Context().Err() == nil {
		t.Fatal("revoked session still open")
	}
}

func TestSubscriptionGuardCases(t *testing.T) {
	hub, _ := newModuleHub("test-realtime", nil)
	t.Cleanup(hub.Cancel)
	session := openSession(t, hub, "alpha")
	if err := session.changeSubscription(session.Context(), nil, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing argument accepted: %v", err)
	}
	if err := session.changeSubscription(session.Context(),
		[]json.RawMessage{json.RawMessage(`{"tenant":"alpha","kind":"tenant","id":"alpha","channel":"updates","oops":1}`)},
		true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown field accepted: %v", err)
	}
	if err := session.changeSubscription(session.Context(), []json.RawMessage{json.RawMessage(`not-json`)},
		true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed argument accepted: %v", err)
	}
	if _, err := session.Invoke(session.Context(), "Unsubscribe", []json.RawMessage{
		json.RawMessage(`{"tenant":"alpha","kind":"tenant","id":"alpha","channel":"missing"}`)}); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("unknown channel unsubscribe accepted: %v", err)
	}
	if err := subscribeChannel(session, "Subscribe", "nope"); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("unknown channel subscribe accepted: %v", err)
	}
}

func subscribeChannel(session *Session, action, channel string) error {
	raw, err := json.Marshal(map[string]string{"tenant": "alpha", "kind": "tenant", "id": "alpha", "channel": channel})
	if err != nil {
		return err
	}
	_, err = session.Invoke(session.Context(), action, []json.RawMessage{raw})
	return err
}

func TestSubscriptionCapacityAndSelectFailure(t *testing.T) {
	authority := &currentAuthority{}
	hub, err := newAuthorityOptionsHub(authority, Options{
		Producer: "test-realtime", Connect: Access{Permission: "connect"},
		Methods:        []Method{echoMethod()},
		Channels:       []Channel{demoChannel(), renamedChannel("other")},
		MaxConnections: 4, MaxSubscriptions: 1, QueueSize: 1, MaxPayload: 1024, MaxOperations: 2,
		OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	session := openSession(t, hub, "alpha")
	if err := subscribeChannel(session, "Subscribe", "updates"); err != nil {
		t.Fatal(err)
	}
	if err := subscribeChannel(session, "Subscribe", "other"); !errors.Is(err, ErrCapacity) {
		t.Fatalf("over-limit subscription accepted: %v", err)
	}
	if _, err := session.Invoke(session.Context(), "Echo", nil); err != nil {
		t.Fatal(err)
	}
	// An owner-selecting method that reports an error denies the invocation
	// without running the handler.
	refused := Method{Name: "Sel", Access: Access{Permission: "sel"},
		Select: func(string, []json.RawMessage) (scope.ScopeRef, error) {
			return scope.ScopeRef{}, errors.New("select refused")
		},
		Invoke: func(context.Context, []json.RawMessage) (any, error) {
			return nil, errors.New("forged handler must not run")
		}}
	hub.methods["sel"] = refused
	if _, err := session.Invoke(session.Context(), "Sel", nil); err == nil {
		t.Fatal("failing owner select accepted")
	}
}

func TestWriteDeliveryGuards(t *testing.T) {
	hub, _ := newModuleHub("test-realtime", nil)
	t.Cleanup(hub.Cancel)
	session := openSession(t, hub, "alpha")
	if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
		t.Fatal(err)
	}
	publishEvent(t, hub, "closed-write")
	session.Close()
	item := <-session.Pending()
	// A closed session cannot admit a final write for a queued delivery.
	if err := session.WriteDelivery(item,
		func(context.Context, string, Notification) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed session write was not rejected: %v", err)
	}
}

func TestWriteDeliveryRemovesDeniedSubscription(t *testing.T) {
	authority := &currentAuthority{}
	hub, err := newAuthorityHub(authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	session := openSession(t, hub, "alpha")
	if err := subscribe(t, session, "Subscribe", "alpha"); err != nil {
		t.Fatal(err)
	}
	publishEvent(t, hub, "denied")
	item := getDelivery(t, session)
	authority.denied.Store(true)
	if err := session.WriteDelivery(item,
		func(context.Context, string, Notification) error { t.Fatal("denied receive wrote"); return nil }); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("unexpected error %v", err)
	}
	// The denied receive removed the subscription, so events later in the
	// same revision stream are never admitted again.
	publishEvent(t, hub, "after-denial")
	noDelivery(t, session)
}

func TestAcceptEventGuards(t *testing.T) {
	hub, _ := newModuleHub("test-realtime", nil)
	t.Cleanup(hub.Cancel)
	long := make([]byte, 130)
	for i := range long {
		long[i] = 'a'
	}
	oversizeID := distributed.Envelope{Type: EventType, SchemaVersion: 1, Producer: "test-realtime", ID: string(long),
		TenantID: "alpha", ScopeKind: "tenant", ScopeID: "alpha", Payload: json.RawMessage(`{"channel":"updates","data":42}`)}
	if err := hub.AcceptEvent(context.Background(), oversizeID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversize id accepted: %v", err)
	}
	if err := hub.Quiesce(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := hub.AcceptEvent(context.Background(), distributed.Envelope{
		Type: EventType, SchemaVersion: 1, Producer: "test-realtime", ID: "after-quiesce",
		TenantID: "alpha", ScopeKind: "tenant", ScopeID: "alpha", Payload: json.RawMessage(`{"channel":"updates","data":42}`),
	})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("quiesced hub accepted an event: %v", err)
	}
}

func TestOperationSlotsDenyOverLimit(t *testing.T) {
	hub, _ := newModuleHub("test-realtime", nil)
	t.Cleanup(hub.Cancel)
	waiter := Method{Name: "Wait", Access: Access{Permission: "wait"},
		Invoke: func(ctx context.Context, _ []json.RawMessage) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
	hub.methods["wait"] = waiter
	session := openSession(t, hub, "alpha")
	done := make(chan error, 4)
	for i := 0; i < hub.options.MaxOperations; i++ {
		go func() { _, err := session.Invoke(session.Context(), "Wait", nil); done <- err }()
	}
	// A third admission must report the operation-slot capacity once the
	// waiters hold every slot.
	time.Sleep(100 * time.Millisecond)
	for attempts := 0; attempts < 25; attempts++ {
		if errors.Is(capacityProbe(session), ErrCapacity) {
			if err := hub.Quiesce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := hub.Drain(t.Context()); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < hub.options.MaxOperations; i++ {
				select {
				case err := <-done:
					// The waiters either see their own operation deadline or
					// the drain cancellation; both close the operations.
					if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("unexpected operation error %v", err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("operation did not drain")
				}
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("over-limit operation was admitted")
}

func newAuthorityOptionsHub(authority *currentAuthority, options Options) (*Hub, error) {
	pipeline, err := identity.NewPipeline(authority)
	if err != nil {
		return nil, err
	}
	resolver, err := tenancy.NewResolver(identity.Identity, authority)
	if err != nil {
		return nil, err
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: authority, Permission: authority.permission})
	if err != nil {
		return nil, err
	}
	return New(options, pipeline, resolver, registry, nil)
}

func capacityProbe(session *Session) error {
	done := make(chan error, 1)
	go func() { _, err := session.Invoke(session.Context(), "Wait", nil); done <- err }()
	select {
	case err := <-done:
		return err
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}
