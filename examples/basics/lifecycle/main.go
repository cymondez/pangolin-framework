package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/kernel/module"
	"github.com/cymondez/pangolin-framework/kernel/runtime"
)

func run() (err error) {
	host, err := runtime.New(runtime.Options{
		DrainTimeout: time.Second, CleanupTimeout: time.Second,
	}, []config.Source{{Name: "defaults", Values: map[string]string{
		"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s",
		"uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4",
	}}})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = errors.Join(err, host.Stop(cleanup))
	}()
	modules := []module.Module{
		{Name: "storage", Version: "0.1.0",
			Start: func(context.Context, module.Runtime) error { fmt.Println("start storage"); return nil },
			Stop:  func(context.Context, module.Runtime) error { fmt.Println("stop storage"); return nil }},
		{Name: "books", Version: "0.1.0",
			DependsOn: []module.Dependency{{Name: "storage", VersionConstraint: "=0.1.0"}},
			Start:     func(context.Context, module.Runtime) error { fmt.Println("start books"); return nil },
			Stop:      func(context.Context, module.Runtime) error { fmt.Println("stop books"); return nil }},
	}
	if err := host.Start(context.Background(), modules); err != nil {
		return err
	}
	fmt.Println("ready:", host.Ready())
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
