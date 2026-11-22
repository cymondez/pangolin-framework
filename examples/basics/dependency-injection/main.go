package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/di"
)

type Greeter interface{ Hello(string) string }
type EnglishGreeter struct{}

func (EnglishGreeter) Hello(name string) string { return "Hello, " + name + "!" }

func run() (err error) {
	ctx := context.Background()
	key := di.NewKey[Greeter]("greeter")
	registry, err := di.NewRegistry(time.Second)
	if err != nil {
		return err
	}
	err = registry.Provide(di.Provider{
		Key: key, Lifetime: di.Singleton,
		Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
			return EnglishGreeter{}, nil, nil
		},
	})
	if err != nil {
		return err
	}
	container, err := registry.Seal(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err = errors.Join(err, container.Close(cleanup))
	}()
	service, err := container.Resolve(ctx, key)
	if err != nil {
		return err
	}
	fmt.Println(service.(Greeter).Hello("Alice"))
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
