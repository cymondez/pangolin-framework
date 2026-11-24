//go:build integration

package m3

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/platform/audit"
	"github.com/cymondez/pangolin-framework/platform/cache"
	"github.com/cymondez/pangolin-framework/platform/settings"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// Reuses the complete governance fixture so ancestry is current SQL authority,
// rather than a test-only reconstruction of parent IDs.
func TestA80PlatformAuthority(t *testing.T) {
	g := governanceFixture(t)
	enterprise := g.newEnterprise(t)
	org := g.newOrg(t, &enterprise.Ref.ID)
	standalone := g.newOrg(t, nil)
	ctx, err := g.registry.Bind(g.ctx, org.Ref)
	if err != nil {
		t.Fatal(err)
	}
	other, err := g.registry.Bind(g.ctx, standalone.Ref)
	if err != nil {
		t.Fatal(err)
	}
	p := settings.NewMemoryProvider(nil)
	memory, _ := cache.NewMemory("app", "0.1.0", 16)
	service, err := settings.New(p, memory, g.store, time.Minute, settings.Definition{Key: "color", Default: json.RawMessage(`"default"`), Inherit: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Put(ctx, settings.Layer{Ref: enterprise.Ref}, "color", json.RawMessage(`"enterprise"`))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("current-parent", func(t *testing.T) {
		result, err := service.Resolve(ctx, "color")
		if err != nil || string(result.Value) != `"enterprise"` {
			t.Fatal(result, err)
		}
		result, err = service.Resolve(other, "color")
		if err != nil || string(result.Value) != `"default"` {
			t.Fatal(result, err)
		}
		binding, _ := scope.Current(ctx)
		parent, ok, err := g.store.Parent(ctx, binding)
		if err != nil || !ok || parent != enterprise.Ref {
			t.Fatal(parent, ok, err)
		}
		if _, _, err = g.store.Parent(context.Background(), binding); err == nil {
			t.Fatal("forged binding")
		}
	})
	t.Run("audit-owner", func(t *testing.T) {
		m := manager(t, &postgres.Resource{Pool: g.app})
		trace := rand.Text()
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			return (audit.Recorder{}).Record(c, audit.Record{Action: "setting.read", Resource: "color", Result: "success", Trace: trace})
		})
		if err != nil {
			t.Fatal(err)
		}
		var tenant, kind, id, principal string
		err = g.admin.QueryRow(context.Background(), "SELECT tenant_id,scope_kind,scope_id,principal_id FROM pangolin_audit.records WHERE trace=$1", trace).Scan(&tenant, &kind, &id, &principal)
		if err != nil || tenant != org.Ref.TenantID || kind != org.Ref.Kind || id != org.Ref.ID || principal != "alice" {
			t.Fatal(tenant, kind, id, principal, err)
		}
	})
	t.Run("revoked-ancestry", func(t *testing.T) {
		_, err := g.admin.Exec(context.Background(), "UPDATE pangolin_organization.organization_memberships SET state='revoked',version=version+1 WHERE tenant_id=$1 AND organization_id=$2", g.tenant, org.Ref.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.Resolve(ctx, "color"); err == nil {
			t.Fatal("stale inherited allow")
		}
	})
}
