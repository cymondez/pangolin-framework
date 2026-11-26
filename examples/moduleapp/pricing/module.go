// Package pricing demonstrates a second consumer of the shared store plus a
// constructor-parameter dependency: the engine resolves *store.Store from the
// constructor signature, never from struct fields or tags.
package pricing

import (
	"context"
	"fmt"

	"example.com/moduleapp/store"
	"github.com/cymondez/pangolin-framework/kernel/module"
)

// Prices is the pricing service. It stays private to this module: without an
// export of its own contract, no other module can resolve it.
type Prices struct {
	store *store.Store
}

// NewPrices is the constructor registered by the pricing module.
func NewPrices(s *store.Store) *Prices {
	return &Prices{store: s}
}

// Estimate returns a fixed demo price for any stored entry.
func (p *Prices) Estimate(id string) (string, error) {
	if _, ok := p.store.Get(id); !ok {
		return "", fmt.Errorf("pricing: unknown catalog entry %q", id)
	}
	return "12.00 EUR", nil
}

// Module is the pricing root-mode module instance.
type Module struct{}

func (m *Module) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "pricing", Version: "0.1.0"}
}

func (m *Module) DependsOn() []module.Ref {
	return []module.Ref{module.Of[store.Module]()}
}

func (m *Module) Configure(b module.RootBuilder) error {
	// Default lifetime Transient would rebuild per resolve; this service has no
	// state worth sharing, demonstrating the default explicitly.
	return b.Services().Add(NewPrices)
}

func (m *Module) Start(ctx context.Context, rt module.Runtime) error {
	// Resolving an own-module concrete does not require an export.
	value, err := rt.Services().Resolve(ctx, module.Contract[*Prices]())
	if err != nil {
		return err
	}
	prices, ok := value.(*Prices)
	if !ok {
		return fmt.Errorf("pricing: unexpected service type %T", value)
	}
	estimate, err := prices.Estimate("demo")
	if err != nil {
		return err
	}
	fmt.Printf("start pricing via %T: %s\n", value, estimate)
	return nil
}

func (m *Module) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop pricing")
	return nil
}
