//go:build integration

package m3

import (
	"context"
	"errors"
	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	transport "github.com/cymondez/pangolin-framework/transport/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestA08UntrustedProducer(t *testing.T) {
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "worker", Kind: "service"}, "a", true
	}
	registry, _ := scope.NewRegistry(identity, scope.TenantScopeProvider{Identity: identity, Permission: func(context.Context, scope.ScopeBinding, string) error { return nil }})
	exec := distributed.Execution{Manager: manager(t, refuseBegin{}), Identity: identity, Resolver: registry, TrustedProducer: func(context.Context, string) error { return scope.ErrDenied }, Authorize: registry.Authorize}
	e := exec.Run(context.Background(), distributed.Task{Scope: scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"}, Producer: "untrusted-session", Permission: "fact.project"}, func(context.Context) error { t.Fatal("untrusted producer executed"); return nil })
	if !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
}

type refuseBegin struct{}

func (refuseBegin) Begin(context.Context, uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	panic("untrusted producer opened a transaction")
}
func TestA56TenantRLS(t *testing.T) {
	admin, app := securityFixture(t)
	a, _ := tenantContext(t, admin, app, "a", scope.PrincipalRef{ID: "alice", Kind: "user"})
	b, _ := tenantContext(t, admin, app, "b", scope.PrincipalRef{ID: "bob", Kind: "user"})
	m := manager(t, &postgres.Resource{Pool: app})
	t.Run("catalog-metadata-isolation", func(t *testing.T) {
		if e := m.Within(a, uow.Options{}, func(c context.Context) error {
			ex, _ := postgres.Current(c, false)
			var n int
			e := ex.QueryRow(c, `SELECT count(*) FROM pangolin.resource_scopes WHERE tenant_id='b'`).Scan(&n)
			if n != 0 {
				t.Fatal("cross-tenant catalog visible")
			}
			return e
		}); e != nil {
			t.Fatal(e)
		}
	})
	if _, e := admin.Exec(context.Background(), `INSERT INTO pangolin_security_qa.parents VALUES('a','tenant','a','same','a',false),('b','tenant','b','same','b',false)`); e != nil {
		t.Fatal(e)
	}
	t.Run("missing-tenant", func(t *testing.T) {
		if _, e := postgres.Current(context.Background(), false); !errors.Is(e, uow.ErrRequired) {
			t.Fatal(e)
		}
	})
	for _, name := range []string{"missing-sql-filter", "cross-tenant-read"} {
		t.Run(name, func(t *testing.T) {
			for i, c := range []context.Context{a, b} {
				if e := m.Within(c, uow.Options{}, func(work context.Context) error {
					ex, _ := postgres.Current(work, false)
					rows, e := ex.Query(work, `SELECT value FROM pangolin_security_qa.parents WHERE id='same'`)
					if e != nil {
						return e
					}
					defer rows.Close()
					var values []string
					for rows.Next() {
						var v string
						if e = rows.Scan(&v); e != nil {
							return e
						}
						values = append(values, v)
					}
					if len(values) != 1 || values[0] != []string{"a", "b"}[i] {
						t.Fatal(values)
					}
					return rows.Err()
				}); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
	for _, name := range []string{"cross-tenant-write", "cross-tenant-soft-delete"} {
		t.Run(name, func(t *testing.T) {
			if e := m.Within(a, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
				ex, _ := postgres.Current(work, true)
				sql := `UPDATE pangolin_security_qa.parents SET value='forged' WHERE tenant_id='b'`
				if name == "cross-tenant-soft-delete" {
					sql = `UPDATE pangolin_security_qa.parents SET deleted=true WHERE tenant_id='b'`
				}
				tag, e := ex.Exec(work, sql)
				if tag.RowsAffected() != 0 {
					t.Fatal("cross tenant mutated")
				}
				return e
			}); e != nil {
				t.Fatal(e)
			}
		})
	}
	t.Run("role-attributes", func(t *testing.T) {
		var unsafe bool
		e := app.QueryRow(context.Background(), `SELECT rolsuper OR rolbypassrls OR current_user=pg_get_userbyid((SELECT relowner FROM pg_class WHERE oid='pangolin_security_qa.parents'::regclass)) FROM pg_roles WHERE rolname=current_user`).Scan(&unsafe)
		if e != nil || unsafe {
			t.Fatal(e, unsafe)
		}
	})
}
func TestA57TenantForeignKey(t *testing.T) {
	admin, app := securityFixture(t)
	a, _ := tenantContext(t, admin, app, "a", scope.PrincipalRef{ID: "alice", Kind: "user"})
	b, _ := tenantContext(t, admin, app, "b", scope.PrincipalRef{ID: "bob", Kind: "user"})
	m := manager(t, &postgres.Resource{Pool: app})
	if _, e := admin.Exec(context.Background(), `INSERT INTO pangolin_security_qa.parents VALUES('a','tenant','a','parent-only-a','a',false)`); e != nil {
		t.Fatal(e)
	}
	var denied error
	for _, tc := range []struct {
		name   string
		c      context.Context
		tenant string
		good   bool
	}{{"cross-tenant-fk", b, "b", false}, {"same-tenant-fk", a, "a", true}} {
		t.Run(tc.name, func(t *testing.T) {
			e := m.Within(tc.c, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
				ex, _ := postgres.Current(work, true)
				_, e := ex.Exec(work, `INSERT INTO pangolin_security_qa.children VALUES($1,'tenant',$1,'child','parent-only-a')`, tc.tenant)
				return e
			})
			if tc.good && e != nil || !tc.good && e == nil {
				t.Fatal(e)
			}
			if !tc.good {
				denied = e
			}
		})
	}
	t.Run("existence-redaction", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/children", nil)
		transport.WriteError(w, r, denied)
		raw := w.Body.String()
		for _, secret := range []string{"parent-only-a", "pangolin_security_qa", "23503", "SQLSTATE"} {
			if strings.Contains(raw, secret) {
				t.Fatal(raw)
			}
		}
	})
}
