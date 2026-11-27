package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/persistence/uow"
)

type recordingStore struct {
	store
	mu        sync.Mutex
	envelopes []distributed.Envelope
}

func (s *recordingStore) Append(ctx context.Context, e distributed.Envelope, raw []byte) error {
	s.mu.Lock()
	s.envelopes = append(s.envelopes, e)
	s.mu.Unlock()
	return s.store.Append(ctx, e, raw)
}

func TestPublishJSONAndForProducer(t *testing.T) {
	registry, err := distributed.NewRegistry(distributed.Schema{
		Type:     "bookstore.book.created",
		Version:  1,
		Required: []string{"id", "title"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &recordingStore{}
	p, err := NewPublisher(registry, s)
	if err != nil {
		t.Fatal(err)
	}
	m := manager(t, resource{publisher: p})
	ctx := bound(t)

	type bookEvent struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}

	// Outside UOW fails closed.
	if err := p.PublishJSON(ctx, EventSpec{Producer: "bookstore", Type: "bookstore.book.created", SchemaVersion: 1}, bookEvent{ID: "1", Title: "Go"}); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("outside UOW = %v", err)
	}
	if err := p.ForProducer("bookstore").PublishJSON(ctx, "bookstore.book.created", 1, bookEvent{ID: "1", Title: "Go"}); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("producer outside UOW = %v", err)
	}

	// ReadOnly UOW fails closed.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error {
		return p.ForProducer("bookstore").PublishJSON(c, "bookstore.book.created", 1, bookEvent{ID: "1", Title: "Go"})
	}); !errors.Is(err, uow.ErrReadOnly) {
		t.Fatalf("read-only UOW = %v", err)
	}

	// Nil publisher / nil ProducerPublisher fail closed.
	var nilPub *Publisher
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		return nilPub.PublishJSON(c, EventSpec{Producer: "bookstore", Type: "bookstore.book.created", SchemaVersion: 1}, bookEvent{ID: "1", Title: "Go"})
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil publisher = %v", err)
	}
	var nilPP *ProducerPublisher
	if err := nilPP.PublishJSON(ctx, "bookstore.book.created", 1, bookEvent{ID: "1", Title: "Go"}); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("nil ProducerPublisher outside UOW = %v", err)
	}
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		return nilPP.PublishJSON(c, "bookstore.book.created", 1, bookEvent{ID: "1", Title: "Go"})
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil ProducerPublisher inside UOW = %v", err)
	}

	// Invalid payload / missing required schema field / empty producer fail closed and poison UOW.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		return p.ForProducer("").PublishJSON(c, "bookstore.book.created", 1, bookEvent{ID: "1", Title: "Go"})
	}); err == nil {
		t.Fatal("empty producer accepted")
	}
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		return p.ForProducer("bookstore").PublishJSON(c, "bookstore.book.created", 1, map[string]string{"id": "1"})
	}); err == nil {
		t.Fatal("missing required schema field accepted")
	}

	// Happy path via ForProducer inside Write UOW: auto-populates owner, ID, timestamp, correlation/causation.
	var opID string
	pp := p.ForProducer("bookstore")
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		cur, ok := uow.Current(c)
		if !ok {
			t.Fatal("missing uow.Current")
		}
		opID = cur.ID()
		return pp.PublishJSON(c, "bookstore.book.created", 1, bookEvent{ID: "b1", Title: "Go"})
	}); err != nil {
		t.Fatal(err)
	}
	if len(s.envelopes) != 1 {
		t.Fatalf("appended = %d, want 1", len(s.envelopes))
	}
	got := s.envelopes[0]
	if got.Producer != "bookstore" || got.Type != "bookstore.book.created" || got.SchemaVersion != 1 ||
		got.TenantID != "t" || got.ScopeKind != "tenant" || got.ScopeID != "t" ||
		got.CorrelationID != opID || got.CausationID != opID || got.ID == "" || got.OccurredAt.IsZero() {
		t.Fatalf("unexpected staged envelope: %+v", got)
	}

	// Option override works (e.g. custom CorrelationID).
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		return pp.PublishJSON(c, "bookstore.book.created", 1, bookEvent{ID: "b2", Title: "Go2"}, distributed.WithCorrelationID("custom-corr"))
	}); err != nil {
		t.Fatal(err)
	}
	if s.envelopes[1].CorrelationID != "custom-corr" {
		t.Fatalf("correlation override = %q", s.envelopes[1].CorrelationID)
	}
}
