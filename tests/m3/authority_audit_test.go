//go:build integration

package m3

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/platform/audit"
	"github.com/cymondez/pangolin-framework/security/scope"
	tenantpg "github.com/cymondez/pangolin-framework/tenancy/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
)

func TestTenantAuthorityProjection(t *testing.T) {
	admin, app := securityFixture(t)
	p := scope.PrincipalRef{ID: "alice", Kind: "user"}
	ctx, _ := tenantContext(t, admin, app, "a", p)
	a := tenantpg.Authority{Pool: app}
	if rev, e := a.Check(ctx, p, "a"); e != nil || rev != "1" {
		t.Fatal(rev, e)
	}
	for _, caseP := range []scope.PrincipalRef{{}, {ID: "alice", Kind: "invalid"}, {ID: "missing", Kind: "user"}} {
		if _, e := a.Check(ctx, caseP, "a"); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
	}
	if _, e := a.Check(nil, p, "a"); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	if _, e := (tenantpg.Authority{}).Check(ctx, p, "a"); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `UPDATE pangolin_security.tenant_memberships SET state='revoked',revision=revision+1 WHERE tenant_id='a'`); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Check(ctx, p, "a"); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `ALTER TABLE pangolin_security.tenant_memberships DROP CONSTRAINT tenant_memberships_revision_check;UPDATE pangolin_security.tenant_memberships SET state='active',revision=0 WHERE tenant_id='a'`); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Check(ctx, p, "a"); !errors.Is(e, scope.ErrAuthorityUnavailable) {
		t.Fatal("corrupt revision allowed", e)
	}
	app.Close()
	if _, e := a.Check(ctx, p, "a"); !errors.Is(e, scope.ErrAuthorityUnavailable) {
		t.Fatal("offline projection allowed", e)
	}
}
func TestA62Audit(t *testing.T) {
	admin, app := securityFixture(t)
	ctx, _ := tenantContext(t, admin, app, "audit", scope.PrincipalRef{ID: "opaque-principal", Kind: "user"})
	m := manager(t, &postgres.Resource{Pool: app})
	record := audit.Recorder{}
	var output bytes.Buffer
	security, _ := audit.NewSecurityLog(&output)
	write := func(c context.Context, id string) error {
		ex, e := postgres.Current(c, true)
		if e != nil {
			return e
		}
		_, e = ex.Exec(c, `INSERT INTO pangolin_security_qa.parents VALUES('audit','tenant','audit',$1,'business',false)`, id)
		if e != nil {
			return e
		}
		return record.Record(c, audit.Record{Action: "books.create", Resource: id, Result: "success", Trace: id})
	}
	counts := func(id string) (int, int) {
		var b, a int
		if e := admin.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM pangolin_security_qa.parents WHERE id=$1),(SELECT count(*) FROM pangolin_audit.records WHERE resource=$1)`, id).Scan(&b, &a); e != nil {
			t.Fatal(e)
		}
		return b, a
	}
	t.Run("success-required-audit", func(t *testing.T) {
		id := rand.Text()
		if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return write(c, id) }); e != nil {
			t.Fatal(e)
		}
		if b, a := counts(id); b != 1 || a != 1 {
			t.Fatal(b, a)
		}
	})
	t.Run("auth-denial-log", func(t *testing.T) {
		if e := security.Write(context.Background(), audit.Record{Action: "books.read", Resource: "opaque-resource", Result: "denied", Trace: "request"}); e != nil || !strings.Contains(output.String(), "denied") {
			t.Fatal(e, output.String())
		}
	})
	t.Run("business-rollback-log", func(t *testing.T) {
		id := rand.Text()
		boom := errors.New("business rejected")
		e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := write(c, id); e != nil {
				return e
			}
			return boom
		})
		if !errors.Is(e, boom) {
			t.Fatal(e)
		}
		if b, a := counts(id); b != 0 || a != 0 {
			t.Fatal(b, a)
		}
		if e = security.Write(ctx, audit.Record{Action: "books.create", Resource: id, Result: "failed", Trace: id}); e != nil || !strings.Contains(output.String(), id) {
			t.Fatal(e)
		}
	})
	t.Run("audit-write-failure", func(t *testing.T) {
		_, e := admin.Exec(context.Background(), `CREATE FUNCTION pangolin_audit.reject_write() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'required audit unavailable';END$$;CREATE TRIGGER reject_audit BEFORE INSERT ON pangolin_audit.records FOR EACH ROW EXECUTE FUNCTION pangolin_audit.reject_write();`)
		if e != nil {
			t.Fatal(e)
		}
		id := rand.Text()
		e = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return write(c, id) })
		if e == nil {
			t.Fatal("audit failure committed")
		}
		if b, a := counts(id); b != 0 || a != 0 {
			t.Fatal(b, a)
		}
		if _, e = admin.Exec(context.Background(), `DROP TRIGGER reject_audit ON pangolin_audit.records;DROP FUNCTION pangolin_audit.reject_write()`); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("telemetry-failure", func(t *testing.T) {
		id := rand.Text()
		e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			s, _ := m.Current(c)
			if e := s.OnCompleted(func(context.Context, uow.Completion) error { return errors.New("telemetry offline") }); e != nil {
				return e
			}
			return write(c, id)
		})
		if e != nil {
			t.Fatal(e)
		}
		if b, a := counts(id); b != 1 || a != 1 {
			t.Fatal(b, a)
		}
	})
}
func TestScopeInventoryFailsClosed(t *testing.T) {
	admin, app := securityFixture(t)
	for _, i := range []postgres.ScopeInventory{{}, {Pool: admin}, {Pool: admin, Tables: []postgres.OwnerTable{{}}}, {Pool: admin, Tables: []postgres.OwnerTable{{Schema: "missing", Table: "missing", KindColumn: "kind"}}}, {Pool: app, Tables: []postgres.OwnerTable{{Schema: "pangolin_security_qa", Table: "parents", KindColumn: "scope_kind"}}}} {
		if _, e := i.RequiredKinds(context.Background()); e == nil {
			t.Fatal("partial inventory accepted", i.Tables)
		}
	}
	if _, e := admin.Exec(context.Background(), `INSERT INTO pangolin_security_qa.jobs VALUES(NULL)`); e != nil {
		t.Fatal(e)
	}
	i := postgres.ScopeInventory{Pool: admin, Tables: []postgres.OwnerTable{{Schema: "pangolin_security_qa", Table: "jobs", KindColumn: "scope_kind"}}}
	if _, e := i.RequiredKinds(context.Background()); e == nil {
		t.Fatal("null kind allowed")
	}
	if _, e := admin.Exec(context.Background(), `UPDATE pangolin_security_qa.jobs SET scope_kind=''`); e != nil {
		t.Fatal(e)
	}
	if _, e := i.RequiredKinds(context.Background()); e == nil {
		t.Fatal("empty kind allowed")
	}
	if _, e := admin.Exec(context.Background(), `UPDATE pangolin_security_qa.jobs SET scope_kind='tenant'`); e != nil {
		t.Fatal(e)
	}
	i.Tables = append(i.Tables, i.Tables[0])
	if kinds, e := i.RequiredKinds(context.Background()); e != nil || len(kinds) != 1 {
		t.Fatal(kinds, e)
	}
}
func TestA09AutomaticReadScopes(t *testing.T) {
	admin, app := securityFixture(t)
	a, _ := tenantContext(t, admin, app, "a", scope.PrincipalRef{ID: "alice", Kind: "user"})
	b, _ := tenantContext(t, admin, app, "b", scope.PrincipalRef{ID: "bob", Kind: "user"})
	m := manager(t, &postgres.Resource{Pool: app})
	t.Run("interleaved-auto-queries", func(t *testing.T) {
		entered, release := make(chan struct{}, 2), make(chan struct{})
		done := make(chan error, 2)
		for _, c := range []context.Context{a, b} {
			go func(c context.Context) {
				done <- m.Within(c, uow.Options{}, func(work context.Context) error {
					current, _ := scope.Current(work)
					ex, _ := postgres.Current(work, false)
					var tenant string
					if e := ex.QueryRow(work, `SELECT current_setting('app.tenant_id')`).Scan(&tenant); e != nil {
						return e
					}
					entered <- struct{}{}
					<-release
					if tenant != current.Ref.TenantID {
						return errors.New("cross-tenant query")
					}
					return nil
				})
			}(c)
		}
		<-entered
		<-entered
		close(release)
		for n := 0; n < 2; n++ {
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		}
	})
	t.Run("connection-reuse", func(t *testing.T) {
		cfg, _ := pgxpool.ParseConfig(app.Config().ConnString())
		cfg.MaxConns = 1
		p, e := pgxpool.NewWithConfig(context.Background(), cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer p.Close()
		single := manager(t, &postgres.Resource{Pool: p})
		for _, c := range []context.Context{a, b, a} {
			if e = single.Within(c, uow.Options{}, func(work context.Context) error {
				current, _ := scope.Current(work)
				ex, _ := postgres.Current(work, false)
				var tenant string
				e := ex.QueryRow(work, `SELECT current_setting('app.tenant_id')`).Scan(&tenant)
				if tenant != current.Ref.TenantID {
					t.Fatal(tenant)
				}
				return e
			}); e != nil {
				t.Fatal(e)
			}
		}
	})
	t.Run("explicit-nontransactional-rejected", func(t *testing.T) {
		if e := m.Within(a, uow.Options{Transaction: uow.NonTransactional}, func(context.Context) error { t.Fatal("RLS without transaction"); return nil }); !errors.Is(e, uow.ErrOptionsInvalid) {
			t.Fatal(e)
		}
	})
}
