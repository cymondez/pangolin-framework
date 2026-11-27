//go:build integration

package m2

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pangolin-framework/pangolin/examples/bookstore/application"
	"github.com/pangolin-framework/pangolin/examples/bookstore/domain"
	"github.com/pangolin-framework/pangolin/examples/bookstore/infrastructure"
	"github.com/pangolin-framework/pangolin/modules/organization"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The thin-slice authority fixture is a real, consistent SQL grant lookup. It
// does not claim the M3 governance Management/default schema implementation.
type sqlAuthority struct{ pool *pgxpool.Pool }

func (a sqlAuthority) Resolve(ctx context.Context, p scope.PrincipalRef, ref scope.ScopeRef) (scope.ScopeBinding, error) {
	var revision string
	e := a.pool.QueryRow(ctx, "SELECT revision FROM pangolin_m2.grants WHERE tenant_id=$1 AND kind=$2 AND id=$3 AND principal=$4", ref.TenantID, ref.Kind, ref.ID, p.ID).Scan(&revision)
	if e != nil {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	return scope.ScopeBinding{Ref: ref, Principal: p, AuthorityRevision: revision}, nil
}
func (a sqlAuthority) Authorize(ctx context.Context, b scope.ScopeBinding, permission string) error {
	_, e := a.Resolve(ctx, b.Principal, b.Ref)
	return e
}

func TestM2ScopeBookstore(t *testing.T) {
	admin := pool(t, "PANGOLIN_M1_ADMIN_DSN")
	app := pool(t, "PANGOLIN_M1_APP_DSN")
	setup(t, admin)
	var exists bool
	if e := admin.QueryRow(context.Background(), "SELECT to_regclass('pangolin.scope_books') IS NOT NULL").Scan(&exists); e != nil {
		t.Fatal(e)
	}
	if !exists {
		raw, e := os.ReadFile(filepath.Join("..", "..", "examples", "bookstore", "infrastructure", "sql", "scoped-schema.sql"))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = admin.Exec(context.Background(), "SET ROLE pangolin_owner;"+string(raw)+";RESET ROLE;"); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := admin.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS pangolin_m2.grants(tenant_id text,kind text,id text,principal text,revision text,PRIMARY KEY(tenant_id,kind,id,principal));
INSERT INTO pangolin_m2.grants VALUES('tenant-a','org.organization','11111111-1111-4111-8111-111111111111','alice','rev1'),('tenant-a','org.organization','22222222-2222-4222-8222-222222222222','alice','rev1'),('tenant-a','org.enterprise','33333333-3333-4333-8333-333333333333','alice','rev1') ON CONFLICT DO NOTHING;
INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) SELECT tenant_id,kind,id FROM pangolin_m2.grants ON CONFLICT DO NOTHING;`); e != nil {
		t.Fatal(e)
	}
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	}
	provider, e := organization.NewProvider(sqlAuthority{admin})
	if e != nil {
		t.Fatal(e)
	}
	resolver, e := scope.NewRegistry(identity, scope.TenantScopeProvider{Identity: identity}, provider)
	if e != nil {
		t.Fatal(e)
	}
	refs := []scope.ScopeRef{{TenantID: "tenant-a", Kind: "org.organization", ID: "11111111-1111-4111-8111-111111111111"}, {TenantID: "tenant-a", Kind: "org.organization", ID: "22222222-2222-4222-8222-222222222222"}, {TenantID: "tenant-a", Kind: "org.enterprise", ID: "33333333-3333-4333-8333-333333333333"}}
	contexts := make([]context.Context, len(refs))
	for i, ref := range refs {
		contexts[i], e = resolver.Bind(context.Background(), ref)
		if e != nil {
			t.Fatal(e)
		}
	}
	m := manager(t, app)
	repo := infrastructure.ScopedRepository{}
	books := application.NewScopedBooks(m, repo, repo)
	id := rand.Text()
	t.Run("S4-exact-owner", func(t *testing.T) {
		if _, e := books.Create(contexts[0], id, "Org A"); e != nil {
			t.Fatal(e)
		}
		for _, ctx := range contexts[1:] {
			if _, e := books.Get(ctx, id); !errors.Is(e, domain.ErrNotFound) {
				t.Fatal("cross-owner read", e)
			}
			if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return repo.Update(c, domain.Book{ID: id, Title: "leak"}, 1) }); !errors.Is(e, domain.ErrConflict) {
				t.Fatal(e)
			}
			if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return repo.Delete(c, id, 1) }); !errors.Is(e, domain.ErrConflict) {
				t.Fatal(e)
			}
		}
		if _, e := books.Create(contexts[1], id, "Org B"); e != nil {
			t.Fatal(e)
		}
		for i, ctx := range contexts[:2] {
			book, e := books.Get(ctx, id)
			if e != nil || book.Title != []string{"Org A", "Org B"}[i] {
				t.Fatal(book, e)
			}
		}
		if e := m.Within(contexts[0], uow.Options{}, func(c context.Context) error {
			ex, _ := postgres.Current(c, false)
			var n int
			if e := ex.QueryRow(c, "SELECT count(*) FROM pangolin.scope_books WHERE id=$1", id).Scan(&n); e != nil {
				return e
			}
			if n != 1 {
				t.Fatal("RLS SQL omitted owner", n)
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		// Tenant-only repositories cannot be used through an Org binding.
		if e := m.Within(contexts[0], uow.Options{}, func(c context.Context) error { _, e := (infrastructure.Repository{}).Get(c, id); return e }); !errors.Is(e, uow.ErrScopeMismatch) {
			t.Fatal(e)
		}
		if e := m.Within(contexts[0], uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return repo.Update(c, domain.Book{ID: id, Title: "A v2"}, 1) }); e != nil {
			t.Fatal(e)
		}
		if e := m.Within(contexts[0], uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return repo.Delete(c, id, 2) }); e != nil {
			t.Fatal(e)
		}
		if _, e := books.Get(contexts[0], id); !errors.Is(e, domain.ErrNotFound) {
			t.Fatal(e)
		}
	})
	for _, prop := range []uow.Propagation{uow.Required, uow.RequiresNew} {
		name := "A76-required-switch"
		if prop == uow.RequiresNew {
			name = "A76-requires-new-switch"
		}
		t.Run(name, func(t *testing.T) {
			err := m.Within(contexts[0], uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
				other, e := resolver.Bind(c, refs[1])
				if e != nil {
					return e
				}
				e = m.Within(other, uow.Options{Propagation: prop}, func(context.Context) error { t.Fatal("changed owner callback"); return nil })
				if !errors.Is(e, uow.ErrScopeMismatch) {
					t.Fatal(e)
				}
				return nil
			})
			if prop == uow.Required && !errors.Is(err, uow.ErrRollbackOnly) {
				t.Fatal(err)
			}
			if prop == uow.RequiresNew && err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("A76-new-authorized-top-level", func(t *testing.T) {
		for _, ctx := range contexts {
			if e := m.Within(ctx, uow.Options{}, func(context.Context) error { return nil }); e != nil {
				t.Fatal(e)
			}
		}
	})
}
