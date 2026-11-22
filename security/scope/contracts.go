// Package scope defines neutral exact-owner references, providers and trusted bindings.
package scope

import "context"

type ScopeRef struct {
	TenantID string
	Kind     string
	ID       string
}
type PrincipalRef struct {
	ID   string
	Kind string
}
type ScopeBinding struct {
	Ref               ScopeRef
	Principal         PrincipalRef
	AuthorityRevision string
}
type ScopeProvider interface {
	Kinds() []string
	Resolve(context.Context, PrincipalRef, ScopeRef) (ScopeBinding, error)
	Authorize(context.Context, ScopeBinding, string) error
}
type ScopeResolver interface {
	Bind(context.Context, ScopeRef) (context.Context, error)
	Current(context.Context) (ScopeBinding, bool)
}

// ScopeAdmitter returns the authoritative snapshot for a new top-level operation.
// It preserves Ref/Principal; RequiresNew reuses its parent's admitted snapshot.
type ScopeAdmitter interface {
	Admit(context.Context, ScopeBinding) (ScopeBinding, error)
}
