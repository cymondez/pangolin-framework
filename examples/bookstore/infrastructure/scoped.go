package infrastructure

import (
	"context"
	"errors"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/examples/bookstore/domain"
	"github.com/pangolin-framework/pangolin/examples/bookstore/infrastructure/internal/scopeddb"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ScopedRepository uses exact owner predicates plus FORCE RLS and catalog FKs.
// Its schema is installed only by the organization-enabled composition.
type ScopedRepository struct{}

func (ScopedRepository) Insert(ctx context.Context, b domain.Book) error {
	e, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	s, _ := scope.Current(ctx)
	err = scopeddb.New(e).InsertScopeBook(ctx, scopeddb.InsertScopeBookParams{TenantID: s.Ref.TenantID, OwnerScopeKind: s.Ref.Kind, OwnerScopeID: s.Ref.ID, ID: b.ID, Title: b.Title, Version: b.Version})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return domain.ErrConflict
	}
	return err
}
func (ScopedRepository) Get(ctx context.Context, id string) (domain.Book, error) {
	e, err := postgres.Current(ctx, false)
	if err != nil {
		return domain.Book{}, err
	}
	s, _ := scope.Current(ctx)
	b, err := scopeddb.New(e).GetScopeBook(ctx, scopeddb.GetScopeBookParams{TenantID: s.Ref.TenantID, OwnerScopeKind: s.Ref.Kind, OwnerScopeID: s.Ref.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return domain.Book{ID: b.ID, Title: b.Title, Version: b.Version}, err
}
func (ScopedRepository) Update(ctx context.Context, b domain.Book, expected int64) error {
	e, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	s, _ := scope.Current(ctx)
	n, err := scopeddb.New(e).UpdateScopeBook(ctx, scopeddb.UpdateScopeBookParams{TenantID: s.Ref.TenantID, OwnerScopeKind: s.Ref.Kind, OwnerScopeID: s.Ref.ID, ID: b.ID, Title: b.Title, Version: expected})
	if err == nil && n != 1 {
		return domain.ErrConflict
	}
	return err
}
func (ScopedRepository) Delete(ctx context.Context, id string, expected int64) error {
	e, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	s, _ := scope.Current(ctx)
	n, err := scopeddb.New(e).DeleteScopeBook(ctx, scopeddb.DeleteScopeBookParams{TenantID: s.Ref.TenantID, OwnerScopeKind: s.Ref.Kind, OwnerScopeID: s.Ref.ID, ID: id, Version: expected})
	if err == nil && n != 1 {
		return domain.ErrConflict
	}
	return err
}
func (ScopedRepository) Publish(ctx context.Context, value any) error {
	e, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	s, _ := scope.Current(ctx)
	event, ok := value.(distributed.Envelope)
	if !ok || event.TenantID != s.Ref.TenantID || event.ScopeKind != s.Ref.Kind || event.ScopeID != s.Ref.ID {
		return uow.Fail(ctx, uow.ErrScopeMismatch)
	}
	raw, err := distributed.Encode(event)
	if err != nil {
		return err
	}
	return scopeddb.New(e).InsertScopeOutbox(ctx, scopeddb.InsertScopeOutboxParams{EventID: event.ID, TenantID: s.Ref.TenantID, ScopeKind: s.Ref.Kind, ScopeID: s.Ref.ID, Envelope: raw})
}
