package scope

import (
	"context"
	"testing"
)

func TestA75Selectors(t *testing.T) {
	ref := ScopeRef{"a", "org.organization", "org-a"}
	for _, tc := range []struct {
		name          string
		route, header ScopeRef
		explicit      bool
		want          error
	}{
		{"header-route-conflict", ref, ScopeRef{"a", "org.organization", "org-b"}, true, ErrHintConflict},
		{"required", ScopeRef{}, ScopeRef{}, true, ErrRequired},
		{"unauthorized-scope", ScopeRef{"b", "org.organization", "org-a"}, ScopeRef{}, true, ErrDenied},
		{"tenant-default", ScopeRef{}, ScopeRef{}, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Select("a", tc.route, tc.header, tc.explicit)
			if err != tc.want {
				t.Fatal(err)
			}
		})
	}
	t.Run("dto-owner-tamper", func(t *testing.T) {
		ctx := bindingVar.With(context.Background(), ScopeBinding{Ref: ref})
		if MatchOwner(ctx, ScopeRef{"a", "org.organization", "org-b"}) != ErrDenied {
			t.Fatal("DTO changed owner")
		}
		if MatchOwner(ctx, ScopeRef{}) != nil || MatchOwner(ctx, ref) != nil {
			t.Fatal("matching owner rejected")
		}
	})
}
