// Package replacement declares the same ports for an independent replacement fixture.
package replacement

import (
	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type Ports struct {
	Scope      scope.ScopeProvider
	Admission  scope.ScopeAdmitter
	Management contracts.Management
}
