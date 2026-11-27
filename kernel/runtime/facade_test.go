package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/di"
	"github.com/pangolin-framework/pangolin/kernel/module"
)

type runtimeFacadeOperation struct{}

func runtimeFacadeSettings() []config.Source {
	return []config.Source{{Name: "defaults", Values: map[string]string{
		"uow.default_timeout":        "5s",
		"uow.cleanup_timeout":        "2s",
		"uow.hook_timeout":           "1s",
		"uow.max_requires_new_depth": "4",
	}}}
}

func TestLegacyRuntimeOpenScope(t *testing.T) {
	host, err := New(Options{DrainTimeout: time.Second, CleanupTimeout: time.Second}, runtimeFacadeSettings())
	if err != nil {
		t.Fatal(err)
	}
	moduleDefinition := module.Module{
		Name:    "legacy-facade",
		Version: "0.1.0",
		Configure: func(builder module.Builder) error {
			return builder.Provide(di.Provider{
				Key:      module.Service[*runtimeFacadeOperation](),
				Lifetime: di.Operation,
				Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
					return &runtimeFacadeOperation{}, nil, nil
				},
			})
		},
		Start: func(ctx context.Context, runtime module.Runtime) error {
			if _, err := runtime.OpenScope(ctx); err != nil {
				return err
			}
			return nil
		},
	}
	if err := host.Start(context.Background(), []module.Module{moduleDefinition}); err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type runtimeFacadeRootModule struct{}

func (*runtimeFacadeRootModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "runtime-facade", Version: "0.1.0"}
}

func (*runtimeFacadeRootModule) Configure(builder module.RootBuilder) error {
	return module.AddOperation(builder.Services(), func() *runtimeFacadeOperation {
		return &runtimeFacadeOperation{}
	})
}

func (*runtimeFacadeRootModule) Start(ctx context.Context, runtime module.Runtime) error {
	scope, err := runtime.OpenScope(ctx)
	if err != nil {
		return err
	}
	defer scope.Close(ctx)
	service, err := module.Resolve[*runtimeFacadeOperation](ctx, scope)
	if err != nil {
		return err
	}
	if service == nil {
		return nil
	}
	return nil
}

func (*runtimeFacadeRootModule) Stop(context.Context, module.Runtime) error { return nil }

func TestRootRuntimeOpenScope(t *testing.T) {
	host, err := New(Options{DrainTimeout: time.Second, CleanupTimeout: time.Second}, runtimeFacadeSettings())
	if err != nil {
		t.Fatal(err)
	}
	if err := StartRoot(context.Background(), host, &runtimeFacadeRootModule{}); err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
