package policy

import (
	"context"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// Host is a separate control-plane path. It grants no session-wide DB bypass.
// The injected query Port must use a dedicated restricted role or audited function.
type Host struct {
	Identity scope.IdentityReader
	Check    func(context.Context, scope.PrincipalRef, string, string) error
	Audit    func(context.Context, scope.PrincipalRef, string, string) error
}

func (h Host) Run(ctx context.Context, targetTenant, action string, query func(context.Context, string) error) error {
	if ctx == nil || h.Identity == nil || h.Check == nil || h.Audit == nil || targetTenant == "" || targetTenant == "*" || action == "" || query == nil {
		return scope.ErrDenied
	}
	p, _, trusted := h.Identity(ctx)
	if !trusted || p.ID == "" || (p.Kind != "user" && p.Kind != "service") {
		return scope.ErrDenied
	}
	if err := h.Check(ctx, p, targetTenant, action); err != nil {
		return err
	}
	if err := h.Audit(ctx, p, targetTenant, action); err != nil {
		return err
	}
	return query(ctx, targetTenant)
}
