// Package replacement declares the same ports for an independent replacement fixture.
package replacement

import (
	"github.com/pangolin-framework/pangolin/modules/organization/contracts"
	"github.com/pangolin-framework/pangolin/security/scope"
)

type Ports struct {
	Scope      scope.ScopeProvider
	Admission  scope.ScopeAdmitter
	Management contracts.Management
}
