// Package application orchestrates books through ports only.
package application

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/examples/bookstore/domain"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

var ErrDenied = errors.New("PERMISSION_DENIED")

type Books struct {
	manager    uow.UnitOfWorkManager
	repository domain.Repository
	publisher  distributed.IntegrationEventPublisher
}

func NewBooks(manager uow.UnitOfWorkManager, repository domain.Repository, publisher distributed.IntegrationEventPublisher) *Books {
	return &Books{manager, repository, publisher}
}
func (s *Books) Create(ctx context.Context, id, title string) (domain.Book, error) {
	binding, ok := scope.Current(ctx)
	if !ok || binding.Ref.Kind != "tenant" {
		return domain.Book{}, ErrDenied
	}
	if id == "" {
		id = rand.Text()
	}
	book, e := domain.NewBook(id, title)
	if e != nil {
		return book, e
	}
	err := s.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite, Propagation: uow.Required}, func(work context.Context) error {
		if e := s.repository.Insert(work, book); e != nil {
			return e
		}
		payload, e := json.Marshal(book)
		if e != nil {
			return e
		}
		return s.publisher.Publish(work, distributed.Envelope{ID: rand.Text(), Type: "bookstore.book_created", SchemaVersion: 1, OccurredAt: time.Now().UTC(), TenantID: binding.Ref.TenantID, ScopeKind: binding.Ref.Kind, ScopeID: binding.Ref.ID, CorrelationID: id, CausationID: id, Producer: "bookstore", Payload: payload})
	})
	return book, err
}
func (s *Books) Get(ctx context.Context, id string) (book domain.Book, err error) {
	if _, ok := scope.Current(ctx); !ok {
		return book, ErrDenied
	}
	err = s.manager.Within(ctx, uow.Options{Mode: uow.ReadOnly}, func(work context.Context) error { var e error; book, e = s.repository.Get(work, id); return e })
	return
}
