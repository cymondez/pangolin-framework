package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/kernel/di"
	"github.com/cymondez/pangolin-framework/kernel/module"
	"github.com/cymondez/pangolin-framework/kernel/runtime"
)

type GreetingService struct{ output io.Writer }

func (s *GreetingService) Greet(name string) error {
	_, err := fmt.Fprintf(s.output, "Hello, %s!\n", name)
	return err
}

func run() (err error) {
	environment := map[string]string{}
	if name, ok := os.LookupEnv("APP_NAME"); ok {
		environment["app.name"] = name
	}
	host, err := runtime.New(runtime.Options{
		DrainTimeout: time.Second, CleanupTimeout: time.Second,
	}, []config.Source{
		{Name: "defaults", Values: map[string]string{
			"app.name": "World", "uow.default_timeout": "5s",
			"uow.cleanup_timeout": "2s", "uow.hook_timeout": "1s",
			"uow.max_requires_new_depth": "4",
		}},
		{Name: "environment", Values: environment},
	})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = errors.Join(err, host.Stop(cleanup))
	}()
	key := module.Service[*GreetingService]()
	app := module.Module{
		Name: "greeting", Version: "0.1.0",
		Configure: func(b module.Builder) error {
			return b.Provide(di.Provider{
				Key: key, Lifetime: di.Singleton,
				Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
					return &GreetingService{output: os.Stdout}, nil, nil
				},
			})
		},
		Start: func(ctx context.Context, app module.Runtime) error {
			name, _ := app.Config().Lookup("app.name")
			service, err := module.Resolve[*GreetingService](ctx, app.Services())
			if err != nil {
				return err
			}
			return service.Greet(name)
		},
	}
	return host.Start(context.Background(), []module.Module{app})
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
