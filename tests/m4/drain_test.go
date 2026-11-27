//go:build integration

package m4

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
	"github.com/pangolin-framework/pangolin/platform/jobs"
)

func TestActiveHTTPWorkerDrain(t *testing.T) {
	for _, forced := range []bool{false, true} {
		name := "normal"
		if forced {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{})
			finish := make(chan struct{})
			cancelled := make(chan struct{})
			var f *fixture
			f = newFixture(t, func(c context.Context, _ jobs.Job) error {
				close(entered)
				if forced {
					<-c.Done()
					close(cancelled)
					<-finish
					return c.Err()
				}
				<-finish
				return f.queue.Ping(c)
			})
			j := f.job(t)
			f.enqueue(t, j)
			worker := f.worker(t, nil)
			httpGroup := runtime.NewWorkGroup()
			var closed atomic.Bool
			var h runtime.Host
			httpEntered := make(chan struct{})
			httpDone := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				work, done, e := httpGroup.Enter(r.Context())
				if e != nil {
					w.WriteHeader(503)
					return
				}
				defer done()
				close(httpEntered)
				if forced {
					<-work.Done()
				} else {
					<-finish
				}
				if closed.Load() {
					t.Error("database closed during HTTP drain")
				}
				_, _ = io.WriteString(w, "done")
			}))
			defer server.Close()
			timeout := 2 * time.Second
			if forced {
				timeout = 50 * time.Millisecond
			}
			var err error
			h, err = runtime.New(runtime.Options{DrainTimeout: timeout, CleanupTimeout: time.Second}, []config.Source{{Name: "defaults", Values: map[string]string{"uow.default_timeout": "5s", "uow.cleanup_timeout": "1s", "uow.hook_timeout": "1s", "uow.max_requires_new_depth": "2"}}}, httpGroup, worker)
			must(t, err)
			must(t, h.Start(context.Background(), []module.Module{{Name: "database", Version: "0.1.0", Stop: func(context.Context, module.Runtime) error {
				closed.Store(true)
				return f.queue.Ping(context.Background())
			}}, {Name: "jobs", Version: "0.1.0", DependsOn: []module.Dependency{{Name: "database", VersionConstraint: "=0.1.0"}}, Start: func(context.Context, module.Runtime) error { worker.Start(); return nil }}}))
			go func() {
				defer close(httpDone)
				response, e := http.Get(server.URL)
				if e == nil {
					response.Body.Close()
				} else {
					t.Error(e)
				}
			}()
			<-entered
			<-httpEntered
			stopped := make(chan error, 1)
			started := time.Now()
			go func() { stopped <- h.Stop(context.Background()) }()
			deadline := time.After(time.Second)
			for h.Ready() {
				select {
				case <-deadline:
					t.Fatal("readiness not withdrawn")
				default:
					time.Sleep(time.Millisecond)
				}
			}
			state, _ := f.state(t, j.ID)
			if state != "processing" {
				t.Fatal("lease released while handler running", state)
			}
			if forced {
				select {
				case <-cancelled:
				case <-time.After(time.Second):
					t.Fatal("handler was not cancelled")
				}
				state, _ = f.state(t, j.ID)
				if state != "processing" || closed.Load() {
					t.Fatal("lease/dependency released before handler cleanup")
				}
			}
			close(finish)
			stopErr := <-stopped
			if forced && !errors.Is(stopErr, context.DeadlineExceeded) {
				t.Fatal(stopErr)
			}
			if !forced {
				must(t, stopErr)
			}
			<-httpDone
			if !closed.Load() {
				t.Fatal("database not stopped")
			}
			state, _ = f.state(t, j.ID)
			want := "succeeded"
			if forced {
				want = "pending"
			}
			if state != want {
				t.Fatal(state, want)
			}
			if _, err = worker.Process(context.Background()); err == nil {
				t.Fatal("claim after quiesce")
			}
			t.Logf("drain_ms=%d forced=%v final_state=%s", time.Since(started).Milliseconds(), forced, state)
			writeEvidence(t, "drain-"+name+".json", map[string]any{"drain_ms": time.Since(started).Milliseconds(), "forced": forced, "final_state": state, "database_last": closed.Load()})
		})
	}
}
