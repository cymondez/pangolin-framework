// Package policy implements deny-by-default, scope-specific authorization.
package policy

import (
	"context"

	"github.com/pangolin-framework/pangolin/security/scope"
)

type Decision uint8

const (
	Abstain Decision = iota
	Allow
	Deny
)

// Rules are one authoritative permission snapshot for exactly one scope.
// Feature/settings are deliberately absent: availability never grants access.
type Rules struct {
	User   Decision
	Roles  []Decision
	Tenant Decision
	System Decision
}

func Allowed(r Rules) bool {
	if r.User == Deny {
		return false
	}
	if r.User == Allow {
		return true
	}
	for _, d := range r.Roles {
		if d == Allow {
			return true
		}
	}
	if r.Tenant != Abstain {
		return r.Tenant == Allow
	}
	return r.System == Allow
}

type Authorizer interface {
	Authorize(context.Context, string) error
}
type ResourcePolicy func(context.Context) error

// Require is also called inside sensitive application use cases. Membership,
// permissions, resource policy and upper restrictions must all permit the action.
func Require(ctx context.Context, a Authorizer, permission string, resource ResourcePolicy) error {
	if ctx == nil || a == nil || permission == "" {
		return scope.ErrDenied
	}
	if _, ok := scope.Current(ctx); !ok {
		return scope.ErrDenied
	}
	if err := a.Authorize(ctx, permission); err != nil {
		return err
	}
	if resource != nil {
		return resource(ctx)
	}
	return nil
}
