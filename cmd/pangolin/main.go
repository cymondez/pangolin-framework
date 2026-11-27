package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/pangolin-framework/pangolin/internal/scaffold"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	dir, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	os.Exit((scaffold.CLI{Dir: dir, Out: os.Stdout, Err: os.Stderr}).Run(ctx, os.Args[1:]))
}
