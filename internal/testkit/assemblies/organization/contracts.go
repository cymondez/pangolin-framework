// Package organization is the organization-enabled assembly contract fixture.
package organization

import (
	"github.com/pangolin-framework/pangolin/modules/organization/contracts"
	"github.com/pangolin-framework/pangolin/security/scope"
)

type Ports struct {
	Scope      scope.ScopeProvider
	Admission  scope.ScopeAdmitter
	Management contracts.Management
}
