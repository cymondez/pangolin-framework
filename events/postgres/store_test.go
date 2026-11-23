package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/events/inbox"
	"github.com/cymondez/pangolin-framework/events/outbox"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPolicy() outbox.Policy {
	return outbox.Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 2}
}
func TestStoreGuardsAndUnavailableDatabase(t *testing.T) {
	ctx := context.Background()
	p, err := pgxpool.New(ctx, "postgres://fixture:fixture@127.0.0.1:1/closed?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	if _, e := NewStore(nil, testPolicy()); e == nil {
		t.Fatal("nil pool")
	}
	if _, e := NewStore(p, outbox.Policy{}); e == nil {
		t.Fatal("policy required")
	}
	s, _ := NewStore(p, testPolicy())
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "service", Kind: "service"}, "t", true
	})
	bound, e := resolver.Bind(ctx, scope.ScopeRef{TenantID: "t", Kind: "tenant", ID: "t"})
	if e != nil {
		t.Fatal(e)
	}
	event := distributed.Envelope{ID: "event", Type: "fact", SchemaVersion: 1, OccurredAt: time.Now(), TenantID: "t", ScopeKind: "tenant", ScopeID: "t", CorrelationID: "op", Producer: "producer", Payload: []byte(`{}`)}
	raw, _ := distributed.Encode(event)
	c := inbox.Candidate{Group: "group", ID: event.ID, Envelope: event, Version: 1}
	for _, fn := range []func() error{
		func() error { return s.Append(ctx, event, []byte(`{}`)) }, func() error { return s.Append(ctx, event, raw) }, func() error { return s.Append(bound, event, raw) },
		func() error { _, e := s.Claim(ctx, "", testPolicy()); return e }, func() error { _, e := s.Claim(ctx, "worker", testPolicy()); return e },
		func() error { return s.Complete(ctx, outbox.Claim{Envelope: event, Token: "token"}, true, 0, "") },
		func() error { return s.Persist(ctx, "", event, raw, "producer") }, func() error { return s.Persist(ctx, "group", event, []byte(`{}`), "producer") }, func() error { return s.Persist(ctx, "group", event, raw, "producer") },
		func() error { _, e := s.Candidate(ctx, "group"); return e }, func() error { _, e := s.Lock(ctx, c); return e }, func() error { return s.Processed(ctx, c) },
		func() error { return s.Retry(ctx, c, 0, 0) }, func() error { return s.Retry(ctx, c, 2, 0) },
	} {
		if err := fn(); err == nil {
			t.Fatal("invalid scope/config/database access accepted")
		}
	}
	allow := func(context.Context, Replay) error { return nil }
	if _, e := NewMaintenance(nil, allow, allow); e == nil {
		t.Fatal("missing store")
	}
	if _, e := NewMaintenance(s, nil, allow); e == nil {
		t.Fatal("missing authorization")
	}
	if _, e := NewMaintenance(s, allow, nil); e == nil {
		t.Fatal("missing audit")
	}
	replay, _ := NewMaintenance(s, allow, allow)
	if e := replay.ReplayDead(ctx, "outbox", "", event.ID); e == nil {
		t.Fatal("missing owner")
	}
	if e := replay.ReplayDead(bound, "outbox", "", event.ID); e == nil {
		t.Fatal("missing UOW")
	}
	denied, _ := NewMaintenance(s, func(context.Context, Replay) error { return scope.ErrDenied }, allow)
	if e := denied.ReplayDead(bound, "outbox", "", event.ID); e == nil {
		t.Fatal("replay policy bypass")
	}
	if _, e := s.Purge(ctx, Retention{}); e == nil {
		t.Fatal("unsafe retention")
	}
	if _, e := s.Purge(ctx, Retention{Broker: time.Hour, ManualReplay: time.Hour, Inbox: time.Hour, OutboxSent: time.Hour}); e == nil {
		t.Fatal("closed pool purge")
	}
}
