package realtime

// Publish covers the staged event path. The optimistic branches and the commit
// boundary run against a real PostgreSQL Outbox when
// PANGOLIN_REALTIME_APP_DSN points at a provisioned database (the shared
// pangolin-realtime-example fixture); without the variable the test skips so
// plain unit runs stay service-free.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/events/outbox"
	eventpg "github.com/pangolin-framework/pangolin/events/postgres"
	pgres "github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5/pgxpool"
)

var scopeErr = scope.ErrDenied

type pgrsc = pgres.Resource

type failingAppendStore struct {
	outbox.Store
	err error
}

func (s failingAppendStore) Append(context.Context, distributed.Envelope, []byte) error { return s.err }

func publishFixture(t *testing.T, publisher *outbox.Publisher) (*Hub, uow.UnitOfWorkManager) {
	t.Helper()
	dsn := os.Getenv("PANGOLIN_REALTIME_APP_DSN")
	if dsn == "" {
		t.Skip("PANGOLIN_REALTIME_APP_DSN not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if publisher == nil {
		registry, err := distributed.NewRegistry(Schema())
		if err != nil {
			t.Fatal(err)
		}
		var store outbox.Store = mustStore(t, pool)
		publisher, err = outbox.NewPublisher(registry, store)
		if err != nil {
			t.Fatal(err)
		}
	}
	hub, err := newModuleHub("test-realtime", publisher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	manager, err := uow.NewManager(&eventpg.Resource{
		Base: &pgrsc{Pool: pool}, Publisher: publisher,
	}, uow.DefaultOptions{
		DefaultTimeout: 3 * time.Second, CleanupTimeout: time.Second,
		HookTimeout: 100 * time.Millisecond, MaxRequiresNewDepth: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return hub, manager
}

func mustStore(t *testing.T, pool *pgxpool.Pool) outbox.Store {
	t.Helper()
	store, err := eventpg.NewStore(pool, outbox.Policy{
		Lease: 15 * time.Second, SendTimeout: 2 * time.Second,
		RetryInitial: 10 * time.Millisecond, RetryMax: 100 * time.Millisecond, MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func publishIn(t *testing.T, manager uow.UnitOfWorkManager, hub *Hub, write bool,
	run func(context.Context) error) error {
	t.Helper()
	credential := identity.Credential{Kind: identity.BearerKind, Value: "alpha"}
	verified, _, err := hub.authenticate(context.Background(), credential)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := hub.bind(verified, tenantScope("alpha"), hub.options.Connect)
	if err != nil {
		t.Fatal(err)
	}
	options := uow.Options{Mode: uow.ReadOnly}
	if write {
		options = uow.Options{Mode: uow.ReadWrite, Transaction: uow.Transactional}
	}
	return manager.Within(admitted, options, func(operation context.Context) error {
		if run == nil {
			return nil
		}
		return run(operation)
	})
}

func TestRealtimePublishRejectsReadOnlyUnit(t *testing.T) {
	hub, manager := publishFixture(t, nil)
	err := publishIn(t, manager, hub, false, func(ctx context.Context) error {
		_, err := hub.Publish(ctx, Destination{Scope: tenantScope("alpha"), Channel: "updates"}, []byte(`{"value":1}`))
		return err
	})
	if !errors.Is(err, uow.ErrReadOnly) {
		t.Fatalf("expected read-only rejection, got %v", err)
	}
}

func TestRealtimePublishStagesOutboxEvent(t *testing.T) {
	hub, manager := publishFixture(t, nil)
	destination := Destination{Scope: tenantScope("alpha"), Channel: "updates"}
	var ids []string
	var failures []error
	err := publishIn(t, manager, hub, true, func(ctx context.Context) error {
		first, err := hub.Publish(ctx, destination, []byte(`{"value":1}`))
		if err != nil {
			failures = append(failures, err)
			return nil
		}
		second, err := hub.Publish(ctx, destination, []byte(`{"value":2}`))
		if err != nil {
			failures = append(failures, err)
			return nil
		}
		ids = []string{first, second}
		return nil
	})
	if len(failures) > 0 {
		for _, failure := range failures {
			t.Errorf("staging failed: %v", failure)
		}
		t.FailNow()
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(ids[0]) == 0 || len(ids[1]) == 0 || ids[0] == ids[1] {
		t.Fatalf("unexpected staged ids %v", ids)
	}
}

func TestRealtimePublishRejectsBadDestinationsAndPayloads(t *testing.T) {
	hub, manager := publishFixture(t, nil)
	base := Destination{Scope: tenantScope("alpha"), Channel: "updates"}
	cases := []struct {
		name        string
		destination Destination
		data        []byte
		want        error
	}{
		{"unknown-channel", Destination{Scope: tenantScope("alpha"), Channel: "other"}, []byte(`{"value":1}`), scopeErr},
		{"foreign-scope", Destination{Scope: tenantScope("beta"), Channel: "updates"}, []byte(`{"value":1}`), scopeErr},
		{"oversize", base, []byte(`{"value":"` + strings.Repeat("x", 1024) + `"}`), ErrInvalid},
		{"invalid-json", base, []byte(`{"value":`), ErrInvalid},
		{"long-user-id", Destination{Scope: tenantScope("alpha"), Channel: "updates", UserID: strings.Repeat("u", 257)},
			[]byte(`{"value":1}`), ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := publishIn(t, manager, hub, true, func(ctx context.Context) error {
				_, err := hub.Publish(ctx, tc.destination, tc.data)
				return err
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("unexpected rejection %v", err)
			}
		})
	}
}

func TestRealtimePublishReportsOutboxAndUnavailableFailures(t *testing.T) {
	cause := errors.New("outbox append failure")
	t.Run("append-failure", func(t *testing.T) {
		registry, err := distributed.NewRegistry(Schema())
		if err != nil {
			t.Fatal(err)
		}
		publisher, err := outbox.NewPublisher(registry, failingAppendStore{err: cause})
		if err != nil {
			t.Fatal(err)
		}
		hub, manager := publishFixture(t, publisher)
		err = publishIn(t, manager, hub, true, func(ctx context.Context) error {
			_, err := hub.Publish(ctx, Destination{Scope: tenantScope("alpha"), Channel: "updates"}, []byte(`{"value":1}`))
			return err
		})
		// Outbox failure poisons the transaction: the unit of work must fail.
		if err == nil {
			t.Fatal("outbox append failure was swallowed")
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		hub, manager := publishFixture(t, nil)
		hub.outbox = nil
		err := publishIn(t, manager, hub, true, func(ctx context.Context) error {
			_, err := hub.Publish(ctx, Destination{Scope: tenantScope("alpha"), Channel: "updates"}, []byte(`{"value":1}`))
			return err
		})
		if !errors.Is(err, outbox.ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})
}
