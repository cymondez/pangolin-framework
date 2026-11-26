// Package app contains the root module of the moduleapp demonstration. The
// root declares its Type dependencies directly; shared dependencies are
// planned once by the loader, and the root only resolves the published
// catalog contract through its restricted per-module resolver.
package app

import (
	"context"
	"fmt"

	"example.com/moduleapp/catalog"
	"example.com/moduleapp/catalog/contracts"
	"example.com/moduleapp/pricing"
	"github.com/cymondez/pangolin-framework/kernel/module"
)

// Module is the application root. It carries no providers of its own: ordinary
// modules only declare Type dependencies plus constructor registrations.
type Module struct{}

func (m *Module) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "app", Version: "0.1.0"}
}

// DependsOn declares the two direct root edges. store is reached through
// catalog and pricing and is planned exactly once.
func (m *Module) DependsOn() []module.Ref {
	return []module.Ref{module.Of[catalog.Module](), module.Of[pricing.Module]()}
}

func (m *Module) Start(ctx context.Context, rt module.Runtime) error {
	// The consumer depends only on the contract owner (catalog) and resolves
	// the exported contract; the implementation module is never a dependency.
	value, err := rt.Services().Resolve(ctx, module.Contract[contracts.Catalog]())
	if err != nil {
		return err
	}
	application, ok := value.(contracts.Catalog)
	if !ok {
		return fmt.Errorf("app: catalog contract is not available: %T", value)
	}
	fmt.Printf("start app: catalog ready with %d entr%s via %T\n",
		application.Len(), plural(application.Len()), value)
	return nil
}

func (m *Module) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop app")
	return nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
