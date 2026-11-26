package realtime

import (
	"context"
	"fmt"

	"github.com/cymondez/pangolin-framework/kernel/module"
	"github.com/cymondez/pangolin-framework/security/identity"
)

// Gateway is the transport contract. Broker ingress is kept on the
// infrastructure-owned Hub instead of being exported to dependent modules.
type Gateway interface {
	Authenticate(context.Context, identity.Credential) (Subject, error)
	Open(context.Context, identity.Credential, Subject) (*Session, error)
	ConnectionLimit() int
	Quiesce(context.Context) error
	Drain(context.Context) error
	Cancel()
}

// Module is the optional Root contract owner. Constructor returns *Hub and
// accepts typed services provided by its direct Dependencies. Infrastructure
// may attach a receiver during StartIngress and return its cleanup.
type Module struct {
	Constructor  any
	Dependencies []module.Ref
	StartIngress func(context.Context, *Hub) (func(context.Context) error, error)

	stopIngress func(context.Context) error
}

func (*Module) ModuleMetadata() module.Metadata { return module.Metadata{Name: "realtime"} }
func (m *Module) DependsOn() []module.Ref       { return m.Dependencies }

func (m *Module) Configure(b module.RootBuilder) error {
	services := b.Services()
	if err := services.Add(m.Constructor, module.Singleton()); err != nil {
		return err
	}
	if err := services.Export(module.Contract[Publisher]()); err != nil {
		return err
	}
	return services.Export(module.Contract[Gateway]())
}

func (m *Module) Start(ctx context.Context, runtime module.Runtime) error {
	service, err := runtime.Services().Resolve(ctx, module.Contract[*Hub]())
	if err != nil {
		return err
	}
	hub, ok := service.(*Hub)
	if !ok || hub == nil {
		return fmt.Errorf("%w: realtime constructor must return *Hub", ErrInvalid)
	}
	if m.StartIngress != nil {
		m.stopIngress, err = m.StartIngress(ctx, hub)
	}
	return err
}

func (m *Module) Stop(ctx context.Context, runtime module.Runtime) error {
	service, err := runtime.Services().Resolve(ctx, module.Contract[*Hub]())
	if err != nil {
		return err
	}
	hub := service.(*Hub)
	_ = hub.Quiesce(ctx)
	hub.Cancel()
	if m.stopIngress != nil {
		return m.stopIngress(ctx)
	}
	return nil
}
