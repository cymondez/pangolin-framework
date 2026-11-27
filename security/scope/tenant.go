package scope

import (
	"context"
	"fmt"

	"github.com/cymondez/pangolin-framework/kernel/ambient"
)

var (
	bindingVar  = ambient.New[ScopeBinding]()
	admitterVar = ambient.New[ScopeAdmitter]()
)

// Admit refreshes the authoritative binding installed by the trusted resolver.
// Only a new root calls it; nested UOWs keep their admission snapshot.
func Admit(ctx context.Context) (context.Context, error) {
	b, ok := Current(ctx)
	if !ok {
		return nil, ErrDenied
	}
	a, ok := admitterVar.Current(ctx)
	if !ok || a == nil {
		return nil, ErrAuthorityUnavailable
	}
	current, err := a.Admit(ctx, b)
	if err != nil {
		return nil, err
	}
	if current.Ref != b.Ref || current.Principal != b.Principal || current.AuthorityRevision == "" {
		return nil, ErrDenied
	}
	return bindingVar.With(ctx, current), nil
}

// Current returns only bindings installed by an authorized resolver.
func Current(ctx context.Context) (ScopeBinding, bool) {
	return bindingVar.Current(ctx)
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
	if e := Validate(ref); e != nil {
		return nil, e
	}
	if ref.Kind != "tenant" {
		return nil, ErrKindUnsupported
	}
	p, tenant, ok := r.identity(ctx)
	if !ok || p.ID == "" || (p.Kind != "user" && p.Kind != "service") || tenant == "" || ref.TenantID != tenant || ref.Kind != "tenant" || ref.ID != tenant {
		return nil, fmt.Errorf("scope access denied")
	}
	b := ScopeBinding{Ref: ref, Principal: p, AuthorityRevision: "tenant-only-m1"}
	return bindingVar.With(admitterVar.With(ctx, r), b), nil
}
func (r *tenantResolver) Current(ctx context.Context) (ScopeBinding, bool) { return Current(ctx) }

func (r *tenantResolver) Admit(ctx context.Context, b ScopeBinding) (ScopeBinding, error) {
	bound, err := r.Bind(ctx, b.Ref)
	if err != nil {
		return ScopeBinding{}, err
	}
	current, _ := Current(bound)
	if current.Principal != b.Principal {
		return ScopeBinding{}, ErrDenied
	}
	return current, nil
}
