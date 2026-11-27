// Command moduleapp is the formal root-composition demonstration: it plans the
// root closure for diagnostics, then hands the root module to app.Run. The
// host starts the full three phase barriers, seals constructor registrations,
// starts modules in topo order and stops them in reverse on ctx cancellation.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	appmod "example.com/moduleapp/app"
	"github.com/pangolin-framework/pangolin/kernel/app"
	"github.com/pangolin-framework/pangolin/kernel/module"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// MODULEAPP_DEADLINE makes the demo exit on its own after the given
	// duration, so the full lifecycle (including reverse Stop) can be
	// observed in non-interactive runs; signal handling stays the default.
	if deadline := os.Getenv("MODULEAPP_DEADLINE"); deadline != "" {
		budget, err := time.ParseDuration(deadline)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid MODULEAPP_DEADLINE:", err)
			os.Exit(1)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}

	// Diagnostics use the public loader: the same deterministic plan the
	// runtime will build, without running any hook or constructor.
	plan, err := module.LoadRoot(&appmod.Module{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("planned layers:")
	for i, layer := range plan.Plan.Layers {
		fmt.Printf("  %d: %v\n", i, layer)
	}

	if err := app.Run(ctx, &appmod.Module{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
