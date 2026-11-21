// Package tenant is the compile-time tenant-only assembly fixture, not an executable application.
package tenant

import "github.com/cymondez/pangolin-framework/security/scope"

type Ports struct {
	Scope     scope.ScopeProvider
	Admission scope.ScopeAdmitter
}
