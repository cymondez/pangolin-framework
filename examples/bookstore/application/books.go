// Package application orchestrates books through ports only.
package application

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	frameworkapplication "github.com/cymondez/pangolin-framework/application"
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
	scopeAware bool
}

func NewBooks(manager uow.UnitOfWorkManager, repository domain.Repository, publisher distributed.IntegrationEventPublisher) *Books {
	return &Books{manager: manager, repository: repository, publisher: publisher}
}

// NewScopedBooks requires the separately installed ScopeAware schema/adapter.
func NewScopedBooks(manager uow.UnitOfWorkManager, repository domain.Repository, publisher distributed.IntegrationEventPublisher) *Books {
	return &Books{manager: manager, repository: repository, publisher: publisher, scopeAware: true}
}
func (s *Books) Create(ctx context.Context, id, title string) (domain.Book, error) {
	binding, ok := scope.Current(ctx)
	if !ok || !s.scopeAware && binding.Ref.Kind != "tenant" {
		return domain.Book{}, ErrDenied
	}
	if id == "" {
		id = rand.Text()
	}
	book, e := domain.NewBook(id, title)
	if e != nil {
		return book, e
	}
	return frameworkapplication.CommandResult(ctx, s.manager, func(work context.Context) (domain.Book, error) {
		if e := s.repository.Insert(work, book); e != nil {
			return domain.Book{}, e
		}
		payload, e := json.Marshal(book)
		if e != nil {
			return domain.Book{}, e
		}
		if e := s.publisher.Publish(work, distributed.Envelope{ID: rand.Text(), Type: "bookstore.book_created", SchemaVersion: 1, OccurredAt: time.Now().UTC(), TenantID: binding.Ref.TenantID, ScopeKind: binding.Ref.Kind, ScopeID: binding.Ref.ID, CorrelationID: id, CausationID: id, Producer: "bookstore", Payload: payload}); e != nil {
			return domain.Book{}, e
		}
		return book, nil
	})
}
func (s *Books) Get(ctx context.Context, id string) (domain.Book, error) {
	if binding, ok := scope.Current(ctx); !ok || !s.scopeAware && binding.Ref.Kind != "tenant" {
		return domain.Book{}, ErrDenied
	}
	return frameworkapplication.QueryResult(ctx, s.manager, func(work context.Context) (domain.Book, error) {
		return s.repository.Get(work, id)
	})
}
