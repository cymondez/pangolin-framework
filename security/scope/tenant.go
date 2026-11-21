package scope

import (
	"context"
	"fmt"
)

type bindingKey struct{}

// Current returns only bindings installed by an authorized resolver.
func Current(ctx context.Context) (ScopeBinding, bool) {
	if ctx == nil {
		return ScopeBinding{}, false
	}
	b, ok := ctx.Value(bindingKey{}).(ScopeBinding)
	return b, ok
}

// IdentityReader is supplied by the trusted authentication adapter at composition.
type IdentityReader func(context.Context) (PrincipalRef, string, bool)
type tenantResolver struct{ identity IdentityReader }

// NewTenantResolver provides the tenant-only M1 profile. Organization kinds are
// rejected; authority revisions for mutable membership are delivered in M2/M3.
func NewTenantResolver(identity IdentityReader) ScopeResolver { return &tenantResolver{identity} }
func (r *tenantResolver) Bind(ctx context.Context, ref ScopeRef) (context.Context, error) {
	if ctx == nil || r.identity == nil {
		return nil, fmt.Errorf("scope identity required")
	}
	p, tenant, ok := r.identity(ctx)
	if !ok || p.ID == "" || tenant == "" || ref.TenantID != tenant || ref.Kind != "tenant" || ref.ID != tenant {
		return nil, fmt.Errorf("scope access denied")
	}
	b := ScopeBinding{Ref: ref, Principal: p, AuthorityRevision: "tenant-only-m1"}
	return context.WithValue(ctx, bindingKey{}, b), nil
}
func (r *tenantResolver) Current(ctx context.Context) (ScopeBinding, bool) { return Current(ctx) }
