//go:build integration

package m3

import (
	"context"
	"github.com/cymondez/pangolin-framework/modules/organization"
	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/security/scope"
	"testing"
	"time"
)

func currentRevision(t *testing.T, g *governance, ref scope.ScopeRef) int64 {
	t.Helper()
	var revision int64
	if e := g.admin.QueryRow(context.Background(), `SELECT version FROM pangolin.resource_scopes WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3`, ref.TenantID, ref.Kind, ref.ID).Scan(&revision); e != nil {
		t.Fatal(e)
	}
	return revision
}
func TestOwnerlessRequiresDedicatedRecovery(t *testing.T) {
	t.Run("enterprise-join-cannot-recover", func(t *testing.T) {
		g := governanceFixture(t)
		parent := g.newEnterprise(t)
		o := g.newOrg(t, &parent.Ref.ID)
		bob := scope.PrincipalRef{ID: "bob", Kind: "user"}
		bobCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, bob)
		if e := g.store.SetEnterpriseRole(g.ctx, contracts.SetEnterpriseRole{Command: g.command(parent.Ref, 1), Principal: bob, Role: "owner"}); e != nil {
			t.Fatal(e)
		}
		opts := g.options
		original := opts.Permissions
		opts.Permissions = organization.PermissionFunc(func(c context.Context, s organization.Snapshot, action string) error {
			if s.Principal == bob && action == "tenant.scope.recover" {
				return scope.ErrDenied
			}
			return original.Check(c, s, action)
		})
		limited, e := organization.NewStore(opts)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = g.admin.Exec(context.Background(), `UPDATE pangolin_security.tenant_memberships SET state='revoked',revision=revision+1 WHERE tenant_id=$1 AND principal_id='alice'`, g.tenant); e != nil {
			t.Fatal(e)
		}
		if e = limited.JoinAsOwner(bobCtx, g.command(o.Ref, currentRevision(t, g, o.Ref))); e == nil {
			t.Fatal("enterprise join revived ownerless organization")
		}
	})
	t.Run("reinstatement-is-not-recovery", func(t *testing.T) {
		g := governanceFixture(t)
		o := g.newOrg(t, nil)
		for _, state := range []string{"revoked", "active"} {
			if _, e := g.admin.Exec(context.Background(), `UPDATE pangolin_security.tenant_memberships SET state=$2,revision=revision+1 WHERE tenant_id=$1 AND principal_id='alice'`, g.tenant, state); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := g.registry.Bind(g.ctx, o.Ref); e == nil {
			t.Fatal("tenant reinstatement revived ownerless scope without recovery")
		}
		if e := g.store.Resume(g.ctx, g.command(o.Ref, currentRevision(t, g, o.Ref))); e == nil {
			t.Fatal("resume bypassed explicit owner recovery")
		}
		if e := g.store.Recover(g.ctx, g.command(o.Ref, currentRevision(t, g, o.Ref)), scope.PrincipalRef{ID: "alice", Kind: "user"}); e != nil {
			t.Fatal(e)
		}
		if _, e := g.registry.Bind(g.ctx, o.Ref); e != nil {
			t.Fatal(e)
		}
	})
}
func TestConcurrentOwnerQualificationRevocations(t *testing.T) {
	g := governanceFixture(t)
	o := g.newOrg(t, nil)
	bob := scope.PrincipalRef{ID: "bob", Kind: "user"}
	tenantContext(t, g.admin, g.app, g.tenant, bob)
	if _, e := g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, 1), Principal: bob, Role: "owner"}); e != nil {
		t.Fatal(e)
	}
	tx, e := g.admin.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(context.Background(), `UPDATE pangolin_security.tenant_memberships SET state='revoked',revision=revision+1 WHERE tenant_id=$1 AND principal_id='alice'`, g.tenant); e != nil {
		t.Fatal(e)
	}
	conn, e := g.admin.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	pid := conn.Conn().PgConn().PID()
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		_, e := conn.Exec(ctx, `UPDATE pangolin_security.tenant_memberships SET state='revoked',revision=revision+1 WHERE tenant_id=$1 AND principal_id='bob'`, g.tenant)
		done <- e
	}()
	deadline := time.Now().Add(3 * time.Second)
	locked := false
	for time.Now().Before(deadline) {
		if e = g.admin.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, pid).Scan(&locked); e != nil {
			t.Fatal(e)
		}
		if locked {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !locked {
		t.Fatal("second revocation did not serialize on scope revision")
	}
	if e = tx.Commit(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	var state, private string
	var revision, privateRevision int64
	if e = g.admin.QueryRow(context.Background(), `SELECT c.state,c.version,o.state,o.version FROM pangolin.resource_scopes c JOIN pangolin_organization.organizations o ON c.tenant_id=o.tenant_id AND c.scope_id=o.id WHERE c.tenant_id=$1 AND c.scope_kind=$2 AND c.scope_id=$3`, g.tenant, o.Ref.Kind, o.Ref.ID).Scan(&state, &revision, &private, &privateRevision); e != nil || state != "ownerless" || private != state || revision != privateRevision || revision != 4 {
		t.Fatal(e, state, revision, private, privateRevision)
	}
}
