package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pangolin-framework/pangolin/examples/bookstore/bootstrap"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e := bootstrap.Run(ctx, "worker", os.Args[1:], os.Stdout, nil); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
