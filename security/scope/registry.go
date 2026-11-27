package scope

import (
	"context"
	"errors"
	"fmt"
	"github.com/cymondez/pangolin-framework/kernel/di"
	"strings"
)

// RegistryKey is the explicit singleton capability used by Runtime at startup.
var RegistryKey = di.NewKey[*Registry]("security.scope.registry")

var ErrInvalid = errors.New("SCOPE_INVALID")
var ErrKindUnsupported = errors.New("SCOPE_KIND_UNSUPPORTED")
var ErrDenied = errors.New("PERMISSION_DENIED")
var ErrAuthorityUnavailable = errors.New("SCOPE_AUTHORITY_UNAVAILABLE")

// Validate enforces complete, exact-owner references. No wildcard is a scope.
func Validate(ref ScopeRef) error {
	for _, s := range []string{ref.TenantID, ref.Kind, ref.ID} {
		if strings.TrimSpace(s) != s || s == "" || s == "*" {
			return ErrInvalid
		}
	}
	if ref.Kind == "tenant" && ref.ID != ref.TenantID {
		return ErrInvalid
	}
	return nil
}

// Registry is sealed at construction. Each kind has exactly one authority.
type Registry struct {
	identity  IdentityReader
	providers map[string]ScopeProvider
}

func NewRegistry(identity IdentityReader, providers ...ScopeProvider) (*Registry, error) {
	if identity == nil {
		return nil, ErrInvalid
	}
	r := &Registry{identity: identity, providers: map[string]ScopeProvider{}}
	for _, p := range providers {
		if p == nil || len(p.Kinds()) == 0 {
			return nil, ErrInvalid
		}
		for _, kind := range p.Kinds() {
			if kind == "" || kind == "*" || strings.TrimSpace(kind) != kind {
				return nil, ErrInvalid
			}
			if _, ok := r.providers[kind]; ok {
				return nil, fmt.Errorf("duplicate scope kind %s", kind)
			}
			r.providers[kind] = p
		}
	}
	return r, nil
}
func (r *Registry) Require(kinds ...string) error {
	for _, k := range kinds {
		if r.providers[k] == nil {
			return fmt.Errorf("%w: %s", ErrKindUnsupported, k)
		}
	}
	return nil
}
func (r *Registry) Bind(ctx context.Context, ref ScopeRef) (context.Context, error) {
	if ctx == nil {
		return nil, ErrInvalid
	}
	if e := Validate(ref); e != nil {
		return nil, e
	}
	p, tenant, ok := r.identity(ctx)
	if !ok || p.ID == "" || (p.Kind != "user" && p.Kind != "service") || tenant != ref.TenantID {
		return nil, ErrDenied
	}
	provider := r.providers[ref.Kind]
	if provider == nil {
		return nil, ErrKindUnsupported
	}
	b, err := provider.Resolve(ctx, p, ref)
	if err != nil {
		return nil, err
	}
	if b.Ref != ref || b.Principal != p || b.AuthorityRevision == "" {
		return nil, ErrDenied
	}
	return bindingVar.With(admitterVar.With(ctx, r), b), nil
}
func (r *Registry) Current(ctx context.Context) (ScopeBinding, bool) { return Current(ctx) }
func (r *Registry) Authorize(ctx context.Context, permission string) error {
	b, ok := Current(ctx)
	if !ok || permission == "" {
		return ErrDenied
	}
	p := r.providers[b.Ref.Kind]
	if p == nil {
		return ErrKindUnsupported
	}
	return p.Authorize(ctx, b, permission)
}
func (r *Registry) Admit(ctx context.Context, b ScopeBinding) (ScopeBinding, error) {
	if ctx == nil {
		return ScopeBinding{}, ErrDenied
	}
	principal, tenant, trusted := r.identity(ctx)
	if !trusted || principal != b.Principal || tenant != b.Ref.TenantID || Validate(b.Ref) != nil {
		return ScopeBinding{}, ErrDenied
	}
	p := r.providers[b.Ref.Kind]
	if p == nil {
		return ScopeBinding{}, ErrKindUnsupported
	}
	current, err := p.Resolve(ctx, b.Principal, b.Ref)
	if err != nil {
		return ScopeBinding{}, err
	}
	if current.Ref != b.Ref || current.Principal != b.Principal || current.AuthorityRevision == "" {
		return ScopeBinding{}, ErrDenied
	}
	return current, nil
}

// TenantScopeProvider depends only on trusted identity; it creates no fake Org.
type TenantScopeProvider struct {
	Identity   IdentityReader
	Permission func(context.Context, ScopeBinding, string) error
}

func (p TenantScopeProvider) Kinds() []string { return []string{"tenant"} }
func (p TenantScopeProvider) Resolve(ctx context.Context, principal PrincipalRef, ref ScopeRef) (ScopeBinding, error) {
	if ctx == nil || p.Identity == nil || Validate(ref) != nil || ref.Kind != "tenant" {
		return ScopeBinding{}, ErrDenied
	}
	actual, tenant, ok := p.Identity(ctx)
	if !ok || actual != principal || ref.TenantID != tenant {
		return ScopeBinding{}, ErrDenied
	}
	return ScopeBinding{Ref: ref, Principal: principal, AuthorityRevision: "tenant-v1"}, nil
}
func (p TenantScopeProvider) Authorize(ctx context.Context, b ScopeBinding, permission string) error {
	if permission == "" || p.Permission == nil {
		return ErrDenied
	}
	current, bound := Current(ctx)
	if !bound || current != b {
		return ErrDenied
	}
	latest, err := p.Resolve(ctx, b.Principal, b.Ref)
	if err != nil {
		return err
	}
	if latest != b {
		return ErrDenied
	}
	return p.Permission(ctx, b, permission)
}
