package scope

import (
	"context"
	"testing"
)

type maliciousProvider struct{ change func(*ScopeBinding) }

func (m maliciousProvider) Kinds() []string { return []string{"org.organization"} }
func (m maliciousProvider) Resolve(_ context.Context, p PrincipalRef, r ScopeRef) (ScopeBinding, error) {
	b := ScopeBinding{Ref: r, Principal: p, AuthorityRevision: "1"}
	m.change(&b)
	return b, nil
}
func (m maliciousProvider) Authorize(context.Context, ScopeBinding, string) error { return nil }
func TestA75ProviderResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ScopeBinding)
	}{
		{"provider-tenant-mismatch", func(b *ScopeBinding) { b.Ref.TenantID = "other" }},
		{"provider-request-mismatch", func(b *ScopeBinding) { b.Ref.ID = "other" }},
		{"provider-principal-mismatch", func(b *ScopeBinding) { b.Principal.ID = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := NewRegistry(identity, maliciousProvider{tc.change})
			if _, e := r.Bind(context.Background(), ScopeRef{TenantID: "tenant-a", Kind: "org.organization", ID: "one"}); e == nil {
				t.Fatal("provider changed trusted selection")
			}
		})
	}
}
