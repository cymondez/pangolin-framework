package uow

import (
	"context"
	"errors"
	"github.com/cymondez/pangolin-framework/security/scope"
	"testing"
)

func TestA44RequiresNewIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, tenant, principal string
		want                    error
	}{{"requires-new-tenant-switch", "tenant-b", "alice", ErrTenantMismatch}, {"requires-new-principal-switch", "tenant-a", "mallory", ErrIdentityMismatch}} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &faultTransaction{}
			m, _ := NewManager(faultResource{tx: tx}, defaults())
			err := m.Within(testContext(t), Options{Mode: ReadWrite}, func(c context.Context) error {
				resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
					return scope.PrincipalRef{ID: tc.principal, Kind: "user"}, tc.tenant, true
				})
				changed, e := resolver.Bind(c, scope.ScopeRef{TenantID: tc.tenant, Kind: "tenant", ID: tc.tenant})
				if e != nil {
					t.Fatal(e)
				}
				e = m.Within(changed, Options{Propagation: RequiresNew}, func(context.Context) error { t.Fatal("changed-identity child began"); return nil })
				if !errors.Is(e, tc.want) {
					t.Fatal(e)
				}
				return nil
			})
			if err != nil || tx.commits != 1 {
				t.Fatal("caught RequiresNew rejection poisoned parent", err, tx.commits)
			}
		})
	}
}
