package infrastructure

import (
	"context"
	"errors"
	"fmt"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/examples/bookstore/domain"
	"github.com/pangolin-framework/pangolin/examples/bookstore/infrastructure/internal/bookdb"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Repository struct{}

func tenantExecutor(ctx context.Context, write bool) (*postgres.Executor, error) {
	if b, ok := scope.Current(ctx); ok && b.Ref.Kind != "tenant" {
		return nil, uow.Fail(ctx, uow.ErrScopeMismatch)
	}
	return postgres.Current(ctx, write)
}

func (Repository) Update(ctx context.Context, b domain.Book, expected int64) error {
	e, err := tenantExecutor(ctx, true)
	if err != nil {
		return err
	}
	binding, _ := scope.Current(ctx)
	n, err := bookdb.New(e).UpdateBook(ctx, bookdb.UpdateBookParams{TenantID: binding.Ref.TenantID, ID: b.ID, Title: b.Title, Version: expected})
	if err == nil && n != 1 {
		return domain.ErrConflict
	}
	return err
}
func (Repository) Delete(ctx context.Context, id string, expected int64) error {
	e, err := tenantExecutor(ctx, true)
	if err != nil {
		return err
	}
	binding, _ := scope.Current(ctx)
	n, err := bookdb.New(e).DeleteBook(ctx, bookdb.DeleteBookParams{TenantID: binding.Ref.TenantID, ID: id, Version: expected})
	if err == nil && n != 1 {
		return domain.ErrConflict
	}
	return err
}

func (Repository) Insert(ctx context.Context, b domain.Book) error {
	e, err := tenantExecutor(ctx, true)
	if err != nil {
		return err
	}
	binding, _ := scope.Current(ctx)
	err = bookdb.New(e).InsertBook(ctx, bookdb.InsertBookParams{TenantID: binding.Ref.TenantID, ID: b.ID, Title: b.Title, Version: b.Version})
	var failure *pgconn.PgError
	if errors.As(err, &failure) && failure.Code == "23505" {
		return domain.ErrConflict
	}
	return err
}
func (Repository) Get(ctx context.Context, id string) (domain.Book, error) {
	e, err := tenantExecutor(ctx, false)
	if err != nil {
		return domain.Book{}, err
	}
	binding, _ := scope.Current(ctx)
	b, err := bookdb.New(e).GetBook(ctx, bookdb.GetBookParams{TenantID: binding.Ref.TenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return domain.Book{ID: b.ID, Title: b.Title, Version: b.Version}, err
}
func (Repository) Publish(ctx context.Context, value any) error {
	e, err := tenantExecutor(ctx, true)
	if err != nil {
		return err
	}
	event, ok := value.(distributed.Envelope)
	if !ok {
		return fmt.Errorf("unsupported integration event")
	}
	binding, _ := scope.Current(ctx)
	if event.TenantID != binding.Ref.TenantID || event.ScopeKind != binding.Ref.Kind || event.ScopeID != binding.Ref.ID {
		return fmt.Errorf("event owner mismatch")
	}
	raw, err := distributed.Encode(event)
	if err != nil {
		return err
	}
	return bookdb.New(e).InsertOutbox(ctx, bookdb.InsertOutboxParams{EventID: event.ID, TenantID: event.TenantID, Envelope: raw})
}
