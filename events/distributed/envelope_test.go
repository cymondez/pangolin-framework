package distributed

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type envStubResource struct{}

func (envStubResource) Begin(ctx context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return ctx, envStubTx{}, nil
}

type envStubTx struct{}

func (envStubTx) Commit(context.Context) error   { return nil }
func (envStubTx) Rollback(context.Context) error { return nil }

func TestNewEnvelope(t *testing.T) {
	// Missing context or scope fails closed.
	if _, err := NewEnvelope(nil, "bookstore", "book.created", 1, map[string]string{"id": "1"}); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("nil ctx = %v", err)
	}
	if _, err := NewEnvelope(context.Background(), "bookstore", "book.created", 1, map[string]string{"id": "1"}); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("unbound ctx = %v", err)
	}

	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	ctx, err := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}

	// Without UOW and without explicit correlation ID, validation rejects empty CorrelationID.
	if _, err := NewEnvelope(ctx, "bookstore", "book.created", 1, map[string]string{"id": "1"}); err == nil {
		t.Fatal("expected error when no UOW and no correlation option")
	}

	// With explicit options outside UOW succeeds and copies raw payload defensively.
	rawIn := json.RawMessage(`{"id":"b1"}`)
	fixedAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	env, err := NewEnvelope(ctx, "bookstore", "book.created", 1, rawIn,
		WithEventID("evt-1"),
		WithOccurredAt(fixedAt),
		WithCorrelationID("corr-1"),
		WithCausationID("cause-1"),
		WithTraceParent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
	)
	if err != nil {
		t.Fatal(err)
	}
	rawIn[2] = 'X'
	if env.ID != "evt-1" || !env.OccurredAt.Equal(fixedAt.UTC()) || env.TenantID != "tenant-a" || env.ScopeKind != "tenant" || env.ScopeID != "tenant-a" ||
		env.CorrelationID != "corr-1" || env.CausationID != "cause-1" || env.Producer != "bookstore" || string(env.Payload) != `{"id":"b1"}` ||
		env.TraceParent != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("unexpected envelope: %+v", env)
	}

	// Inside active UOW: CorrelationID and CausationID auto-populate from uow.Current(ctx).ID().
	m, err := uow.NewManager(envStubResource{}, uow.DefaultOptions{
		DefaultTimeout:      time.Second,
		CleanupTimeout:      time.Second,
		HookTimeout:         time.Second,
		MaxRequiresNewDepth: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	type payloadDTO struct {
		ID string `json:"id"`
	}
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		cur, ok := uow.Current(c)
		if !ok {
			t.Fatal("uow.Current returned false inside Within")
		}
		built, err := NewEnvelope(c, "bookstore", "book.created", 1, payloadDTO{ID: "b2"})
		if err != nil {
			return err
		}
		if built.ID == "" || built.OccurredAt.IsZero() || built.CorrelationID != cur.ID() || built.CausationID != cur.ID() || string(built.Payload) != `{"id":"b2"}` {
			t.Fatalf("unexpected UOW envelope: %+v", built)
		}
		// []byte payload path + non-object payload rejection + unmarshalable payload rejection.
		if _, err := NewEnvelope(c, "bookstore", "book.created", 1, []byte(`{"id":"b3"}`)); err != nil {
			return err
		}
		if _, err := NewEnvelope(c, "bookstore", "book.created", 1, []string{"not-an-object"}); err == nil {
			t.Fatal("non-object JSON accepted")
		}
		if _, err := NewEnvelope(c, "bookstore", "book.created", 1, make(chan int)); err == nil {
			t.Fatal("unmarshalable payload accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
