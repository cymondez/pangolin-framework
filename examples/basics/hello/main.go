package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/app"
	"github.com/cymondez/pangolin-framework/kernel/module"
)

// helloModule is the first panGOlin module: module.Base carries the name and
// version, Start holds the work executed when the application starts.
type helloModule struct {
	module.Base
}

func (m *helloModule) Start(context.Context, module.Runtime) error {
	fmt.Println("Hello, panGOlin!")
	return nil
}

func main() {
	// app.Run waits until ctx is done; the one-second deadline ends this
	// console demonstration after the module finished its Start work.
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	root := &helloModule{Base: module.Base{Name: "hello", Version: "0.1.0"}}
	if err := app.Run(ctx, root); err != nil {
		log.Fatal(err)
	}
}
