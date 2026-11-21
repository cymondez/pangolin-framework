// Package organization is the organization-enabled assembly contract fixture.
package organization

import (
	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type Ports struct {
	Scope      scope.ScopeProvider
	Admission  scope.ScopeAdmitter
	Management contracts.Management
}
