// Package authn verifies the concrete M1 credential adapters: the RS256 JWT
// bearer validator and the deployment-selected service verifier. This file
// keeps the published names working as explicit forwarding adapters over the
// protocol-independent identity contract in security/identity; new
// authentication modules implement identity.Verifier there without importing
// JWT parsing.
package authn

import (
	"context"

	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
)

// ErrUnauthenticated is the same fail-closed sentinel used by the identity
// pipeline; existing errors.Is callers keep working.
var ErrUnauthenticated = identity.ErrUnauthenticated

// Identity forwards to the trusted identity package storage. Resolvers keep
// receiving (principal, tenant, installed) exactly as before.
func Identity(ctx context.Context) (scope.PrincipalRef, string, bool) {
	return identity.Identity(ctx)
}
