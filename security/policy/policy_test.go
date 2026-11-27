package policy

import (
	"context"
	"errors"
	"testing"

	"github.com/pangolin-framework/pangolin/security/scope"
)

func TestA13Permission(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules Rules
		allow bool
	}{
		{"allow", Rules{User: Allow}, true},
		{"explicit-user-deny", Rules{User: Deny, Roles: []Decision{Allow}, Tenant: Allow, System: Allow}, false},
		{"role-tenant-matrix", Rules{Roles: []Decision{Abstain}, Tenant: Allow}, true},
		{"default-deny", Rules{}, false},
		{"role-allow", Rules{Roles: []Decision{Allow}}, true},
		{"system-allow", Rules{System: Allow}, true},
		{"feature-not-permission", Rules{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if Allowed(tc.rules) != tc.allow {
				t.Fatal(tc.rules)
			}
		})
	}
	t.Run("direct-use-case", func(t *testing.T) {
		if err := Require(context.Background(), nil, "books.write", nil); !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
	})
}
