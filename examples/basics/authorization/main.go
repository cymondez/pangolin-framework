package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cymondez/pangolin-framework/security/policy"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
)

// These demo fixtures replace an external IdP and its current membership source
// only for this permission example. They do not verify real credentials.
func demoIdentity(context.Context) (scope.PrincipalRef, string, bool) {
	return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
}

type demoAuthority struct{}

func (demoAuthority) Check(_ context.Context, principal scope.PrincipalRef, tenant string) (string, error) {
	if principal != (scope.PrincipalRef{ID: "alice", Kind: "user"}) || tenant != "tenant-a" {
		return "", scope.ErrDenied
	}
	return "demo-membership-1", nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	authority := demoAuthority{}
	resolver, err := tenancy.NewResolver(demoIdentity, authority)
	if err != nil {
		return err
	}
	ctx, err := resolver.Resolve(context.Background(), "tenant-a", "tenant-a")
	if err != nil {
		return err
	}

	rules := map[string]policy.Rules{
		"books.read": {Roles: []policy.Decision{policy.Allow}},
		"books.write": {
			User: policy.Deny, Roles: []policy.Decision{policy.Allow},
			Tenant: policy.Allow, System: policy.Allow,
		},
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
		Authority: authority,
		Permission: func(_ context.Context, _ scope.ScopeBinding, permission string) error {
			if !policy.Allowed(rules[permission]) {
				return scope.ErrDenied
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	ctx, err = registry.Bind(ctx, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		return err
	}

	// Sensitive application use cases call Require themselves, even when their
	// transport already checked the same permission.
	if err = policy.Require(ctx, registry, "books.read", nil); err != nil {
		return err
	}
	fmt.Println("read allowed")
	if err = policy.Require(ctx, registry, "books.write", nil); !errors.Is(err, scope.ErrDenied) {
		return fmt.Errorf("expected explicit user deny, got %v", err)
	}
	fmt.Println("write denied")

	featureEnabled := true
	if featureEnabled {
		if err = policy.Require(ctx, registry, "books.write", nil); !errors.Is(err, scope.ErrDenied) {
			return fmt.Errorf("feature must not grant write permission, got %v", err)
		}
		fmt.Println("feature enabled: write denied")
	}
	return nil
}
