package infrastructure

import (
	"context"
	"errors"
	"fmt"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/examples/bookstore/domain"
	"github.com/cymondez/pangolin-framework/examples/bookstore/infrastructure/internal/bookdb"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Repository struct{}

func (Repository) Insert(ctx context.Context, b domain.Book) error {
	e, err := postgres.Current(ctx, true)
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
	e, err := postgres.Current(ctx, false)
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
	e, err := postgres.Current(ctx, true)
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
