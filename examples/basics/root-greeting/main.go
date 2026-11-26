package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/app"
	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/kernel/module"
)

// GreetingService is your own service: a plain concrete type needs no export
// and no interface. NewGreetingService is the constructor the module
// registers; its exact return type is the resolution key.
type GreetingService struct{}

func NewGreetingService() *GreetingService { return &GreetingService{} }

func (s *GreetingService) Greet(name string) error {
	_, err := fmt.Fprintf(os.Stdout, "Hello, %s!\n", name)
	return err
}

type greetingModule struct {
	module.Base
}

// Configure declares the module identity work: the app.name default for the
// other layers to override and the service constructor registration.
func (m *greetingModule) Configure(b module.RootBuilder) error {
	if err := b.SetDefault("app.name", "World"); err != nil {
		return err
	}
	// module.Singleton keeps one instance for the whole application; without
	// it the default lifetime builds a fresh instance per resolve.
	return b.Services().Add(NewGreetingService, module.Singleton())
}

func (m *greetingModule) Start(ctx context.Context, rt module.Runtime) error {
	name, _ := rt.Config().Lookup("app.name")
	service, err := rt.Services().Resolve(ctx, module.Contract[*GreetingService]())
	if err != nil {
		return err
	}
	return service.(*GreetingService).Greet(name)
}

func main() {
	// app.Run waits until ctx is done; the one-second deadline ends this
	// console demonstration after the module finished its Start work.
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	env := map[string]string{}
	if name, ok := os.LookupEnv("APP_NAME"); ok {
		env["app.name"] = name
	}
	root := &greetingModule{Base: module.Base{Name: "greeting", Version: "0.1.0"}}
	if err := app.Run(ctx, root, app.Sources(config.Source{Name: "environment", Values: env})); err != nil {
		log.Fatal(err)
	}
}
