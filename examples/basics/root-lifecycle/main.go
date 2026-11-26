package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/app"
	"github.com/cymondez/pangolin-framework/kernel/module"
)

type storageModule struct{}

func (m *storageModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "storage", Version: "0.1.0"}
}

func (m *storageModule) Start(context.Context, module.Runtime) error {
	fmt.Println("start storage")
	return nil
}

func (m *storageModule) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop storage")
	return nil
}

type booksModule struct{}

func (m *booksModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "books", Version: "0.1.0"}
}

// DependsOn declares the dependency by type. The root loader pulls storage
// in, deduplicates shared dependencies and runs Start from storage to books.
func (m *booksModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[storageModule]()}
}

func (m *booksModule) Start(context.Context, module.Runtime) error {
	fmt.Println("start books")
	return nil
}

func (m *booksModule) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop books")
	return nil
}

func main() {
	// app.Run waits until ctx is done; the one-second deadline ends this
	// console demonstration after both modules completed their Start work.
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	// Only the root module is handed to Run: storage arrives through the
	// type reference in books.DependsOn, so the loader also stops it first.
	if err := app.Run(ctx, &booksModule{}); err != nil {
		log.Fatal(err)
	}
}
