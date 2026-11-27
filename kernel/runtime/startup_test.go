package runtime

import (
	"context"
	"errors"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"testing"
	"time"
)

func TestStartupChecksBeforeAdmission(t *testing.T) {
	started := false
	boom := errors.New("schema incompatible")
	checks := []func(context.Context, module.Runtime) error{func(context.Context, module.Runtime) error { return boom }}
	h, e := New(Options{DrainTimeout: time.Second, CleanupTimeout: time.Second, StartupChecks: checks}, settings())
	if e != nil {
		t.Fatal(e)
	}
	checks[0] = nil // Options snapshots the caller's slice.
	e = h.Start(context.Background(), []module.Module{{Name: "api", Version: "0.1.0", Start: func(context.Context, module.Runtime) error { started = true; return nil }}})
	if !errors.Is(e, boom) || started || h.Ready() {
		t.Fatal(e, started)
	}
	if _, e = New(Options{DrainTimeout: time.Second, CleanupTimeout: time.Second, StartupChecks: []func(context.Context, module.Runtime) error{nil}}, settings()); e == nil {
		t.Fatal("nil startup check")
	}
	h, e = New(Options{DrainTimeout: time.Second, CleanupTimeout: time.Second, StartupChecks: []func(context.Context, module.Runtime) error{func(context.Context, module.Runtime) error { return nil }}}, settings())
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Start(context.Background(), nil); e != nil || !h.Ready() {
		t.Fatal(e)
	}
	if e = h.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
}
