package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/module"
)

type appTestModule struct {
	mu           sync.Mutex
	started      bool
	defaultsSeen string
	startedCh    chan struct{}
	start        func(context.Context, module.Runtime) error
}

func newAppTestModule() *appTestModule {
	return &appTestModule{startedCh: make(chan struct{})}
}

func (m *appTestModule) ModuleMetadata() module.Metadata { return module.Metadata{Name: "apptest"} }

func (m *appTestModule) Configure(b module.RootBuilder) error {
	return b.SetDefault("app.name", "builtin")
}

// Start publishes started and defaultsSeen under one mutex section and closes
// startedCh after both writes, so a received signal proves both are visible;
// separate plain writes raced the detector across the Run goroutine boundary.
func (m *appTestModule) Start(ctx context.Context, rt module.Runtime) error {
	m.mu.Lock()
	m.started = true
	name, _ := rt.Config().Lookup("app.name")
	m.defaultsSeen = name
	close(m.startedCh)
	m.mu.Unlock()
	if m.start != nil {
		return m.start(ctx, rt)
	}
	return nil
}

func (m *appTestModule) startSnapshot() (started bool, defaults string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started, m.defaultsSeen
}

func (m *appTestModule) startedNow() bool {
	started, _ := m.startSnapshot()
	return started
}

// Bounded test budgets only: 5s start wait and 10s result wait, as before.
const (
	testStartWait = 5 * time.Second
	testRunWait   = 10 * time.Second
)

// waitAppStart waits bounded until Start published its state, replacing the
// unsynchronized started-flag polling; the receive happens after the mutex
// section closed startedCh, so defaultsSeen is visible whenever started is.
func waitAppStart(t *testing.T, m *appTestModule) (started bool, defaults string) {
	t.Helper()
	timer := time.NewTimer(testStartWait)
	defer timer.Stop()
	select {
	case <-m.startedCh:
	case <-timer.C:
	}
	return m.startSnapshot()
}

// pureCancellation reports whether err carries only the context cancellation,
// with no foreign failure hidden behind it. The tests cancel right after the
// module Start signal, and a cancel landing in the runtime's post-start
// pre-ready window makes StartRoot return ctx.Err() joined with the host
// shutdown result, so Run legitimately reports pure context.Canceled (errors.
// Join discards the nil shutdown member). Join members are checked
// individually and wrapped chains must carry cancellation-only text, so any
// shutdown or cleanup failure inside the chain still fails the test.
func pureCancellation(err error) bool {
	if err == nil {
		return false
	}
	type joinUnwrap interface{ Unwrap() []error }
	if joined, ok := err.(joinUnwrap); ok {
		for _, cause := range joined.Unwrap() {
			if !pureCancellation(cause) {
				return false
			}
		}
		return true
	}
	return errors.Is(err, context.Canceled) && err.Error() == context.Canceled.Error()
}

// A caller "defaults" source values override the built-in defaults inside the
// same layer; other layers keep their usual higher precedence.
func TestRunCallerDefaultsOverrideBuiltins(t *testing.T) {
	t.Setenv("APP_NAME", "")
	merged, _ := mergedDefaults([]config.Source{
		{Name: "defaults", Values: map[string]string{"app.name": "caller"}},
		{Name: "environment", Values: map[string]string{"app.name": "env"}},
	})
	if merged["app.name"] != "caller" {
		t.Fatalf("caller defaults must override built-ins: %v", merged)
	}
}

// With an active context: the caller defaults value must reach the module
// Config and the built-in defaults must not leak past a caller override.
func TestRunCallerDefaultsReachModuleConfig(t *testing.T) {
	app := newAppTestModule()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, app, Sources(
			config.Source{Name: "defaults", Values: map[string]string{"app.name": "caller"}},
		))
	}()
	started, defaults := waitAppStart(t, app)
	if !started {
		t.Fatal("Run did not start the composition")
	}
	if defaults != "caller" {
		t.Fatalf("caller default did not reach module config: %q", defaults)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !pureCancellation(err) {
			t.Fatal(err)
		}
	case <-time.After(testRunWait):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestMergedDefaultsNoAlias(t *testing.T) {
	caller := map[string]string{"app.name": "caller"}
	merged, rest := mergedDefaults([]config.Source{
		{Name: "defaults", Values: caller},
		{Name: "environment", Values: map[string]string{"app.name": "env"}},
	})
	merged["app.name"] = "mutated"
	if caller["app.name"] != "caller" {
		t.Fatal("caller map must not be aliased or mutated")
	}
	if builtinDefaults["uow.default_timeout"] != "5s" {
		t.Fatal("builtin map must not be mutated")
	}
	if len(rest) != 1 || rest[0].Name != "environment" {
		t.Fatalf("non-defaults sources must be preserved: %v", rest)
	}
}

// Invalid caller override values fail StartupRules validation before any hook.
func TestRunInvalidCallerDefaultFails(t *testing.T) {
	app := &appTestModule{}
	err := Run(context.Background(), app, Sources(
		config.Source{Name: "defaults", Values: map[string]string{"uow.max_requires_new_depth": "0"}},
	))
	if err == nil || !strings.Contains(err.Error(), "uow.max_requires_new_depth") {
		t.Fatalf("invalid override must fail config validation: %v", err)
	}
	if app.startedNow() {
		t.Fatal("hooks must not run for an invalid configuration")
	}
}

func TestRunNilContext(t *testing.T) {
	var ctx context.Context
	if err := Run(ctx, &appTestModule{}); err == nil || err.Error() != "context required" {
		t.Fatalf("nil context must be rejected: %v", err)
	}
}

// Wait-for-cancellation: Run blocks until ctx cancel then runs the bounded
// Stop. A pure cancellation return is the legitimate cancel race with the
// post-start pre-ready window (see pureCancellation); any other error fails,
// and the assertions below are unchanged.
func TestRunStopsOnContextCancellation(t *testing.T) {
	app := newAppTestModule()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, app) }()
	started, _ := waitAppStart(t, app)
	if !started {
		t.Fatal("Run did not start the composition")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !pureCancellation(err) {
			t.Fatal(err)
		}
	case <-time.After(testRunWait):
		t.Fatal("Run did not return after cancellation")
	}
}

// appRunParticipant records the drain participants registered through the
// Participants RunOption.
type appRunParticipant struct {
	quiesced, drained atomic.Bool
}

func (p *appRunParticipant) Quiesce(context.Context) error { p.quiesced.Store(true); return nil }
func (p *appRunParticipant) Drain(context.Context) error   { p.drained.Store(true); return nil }
func (p *appRunParticipant) Cancel()                       {}

// Participants registered through the RunOption must be drained at the
// cancellation stop.
func TestRunParticipantsDrainOnCancellation(t *testing.T) {
	app := newAppTestModule()
	participant := &appRunParticipant{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, app, Participants(participant)) }()
	started, _ := waitAppStart(t, app)
	if !started {
		t.Fatal("Run did not start the composition")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !pureCancellation(err) {
			t.Fatal(err)
		}
	case <-time.After(testRunWait):
		t.Fatal("Run did not return after cancellation")
	}
	// Stop and the StartRoot failure rollback both run the host shutdown, so
	// the participants must be drained on the nil and the pure-cancellation
	// outcome alike.
	if !participant.quiesced.Load() || !participant.drained.Load() {
		t.Fatal("registered participants must run at stop")
	}
}

// The StartupChecks RunOption wires post-seal pre-Start checks: a passing
// check runs before the module Start.
func TestRunPassingStartupCheckRuns(t *testing.T) {
	app := newAppTestModule()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	var ran atomic.Bool
	go func() {
		errCh <- Run(ctx, app, StartupChecks(func(context.Context, module.Runtime) error {
			ran.Store(true)
			return nil
		}))
	}()
	// The checks run on the Run goroutine before Start, and receiving
	// startedCh happens after Start, so ran is visible here; the atomic keeps
	// the read race-clean regardless of assertion timing.
	started, _ := waitAppStart(t, app)
	if !ran.Load() || !started {
		t.Fatalf("passing check must run before Start: check=%v started=%v", ran.Load(), started)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !pureCancellation(err) {
			t.Fatal(err)
		}
	case <-time.After(testRunWait):
		t.Fatal("Run did not return after cancellation")
	}
}

// A failing startup check rejects the whole run before any module starts.
func TestRunFailingStartupCheckRejectsStart(t *testing.T) {
	app := &appTestModule{}
	ran := false
	err := Run(context.Background(), app, StartupChecks(func(context.Context, module.Runtime) error {
		ran = true
		return fmt.Errorf("check boom")
	}))
	if err == nil || !strings.Contains(err.Error(), "startup compatibility") || !strings.Contains(err.Error(), "check boom") {
		t.Fatalf("failing startup check must reject the run: %v", err)
	}
	if !ran {
		t.Fatal("the failing check never ran")
	}
	if app.startedNow() {
		t.Fatal("modules must not start when a startup check fails")
	}
}
