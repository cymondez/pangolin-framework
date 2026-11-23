//go:build integration

package m4

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/platform/jobs"
	jobpg "github.com/cymondez/pangolin-framework/platform/jobs/postgres"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type sample struct {
	ElapsedMS                 int64 `json:"elapsed_ms"`
	Pending, Processing, Dead int64
	PoolAcquired              int32  `json:"pool_acquired"`
	PoolMax                   int32  `json:"pool_max"`
	HeapBytes                 uint64 `json:"heap_bytes"`
	Completed                 int32  `json:"completed"`
}

func TestCapacityLoad(t *testing.T) {
	const records = 10000
	const concurrency = 64
	var completed atomic.Int32
	f := newFixture(t, func(context.Context, jobs.Job) error { completed.Add(1); return nil })
	f.policy.MaxPending = records
	f.policy.Concurrency = 4
	f.policy.BatchSize = 32
	f.policy.PollInterval = time.Millisecond
	var err error
	f.store, err = jobpg.NewStore(f.queue, f.registry, f.policy, func(context.Context, string) error { return nil })
	must(t, err)
	prototype := f.job(t)
	prototype.Deadline = time.Now().Add(10 * time.Minute)
	ctx := f.bind(t, context.Background(), f.tenant, prototype.Actor)
	start := make(chan struct{})
	errs := make(chan error, concurrency)
	var wg sync.WaitGroup
	var latenciesMu sync.Mutex
	var latencies []int64
	began := time.Now()
	// 64 competing producers enqueue 10,000 durable records, in bounded chunks.
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for n := worker; n < records; n += concurrency {
				j := prototype
				j.ID = prototype.ID + "-" + time.Unix(0, int64(n)).Format("150405.000000000")
				began := time.Now()
				err := f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, j) })
				if err != nil {
					errs <- err
					return
				}
				latenciesMu.Lock()
				latencies = append(latencies, time.Since(began).Microseconds())
				latenciesMu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	st, err := f.store.Stats(context.Background())
	must(t, err)
	if st.Pending != records {
		t.Fatal(st)
	}
	var samples []sample
	take := func() {
		st, e := f.store.Stats(context.Background())
		must(t, e)
		if st.Processing > int64(f.policy.Concurrency) {
			t.Fatal("unbounded worker claims", st.Processing)
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		pool := f.queue.Stat()
		samples = append(samples, sample{time.Since(began).Milliseconds(), st.Pending, st.Processing, st.Dead, pool.AcquiredConns(), pool.MaxConns(), m.HeapAlloc, completed.Load()})
	}
	take()
	worker := f.worker(t, nil)
	worker.Start()
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for completed.Load() < records {
		select {
		case <-deadline.C:
			t.Fatal("load did not drain", completed.Load())
		case <-ticker.C:
			take()
		}
	}
	must(t, worker.Quiesce(context.Background()))
	drain, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	drainStarted := time.Now()
	must(t, worker.Drain(drain))
	take()
	st, err = f.store.Stats(context.Background())
	must(t, err)
	if st.Pending+st.Processing+st.Dead != 0 {
		t.Fatal(st)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	report := map[string]any{"records": records, "concurrency": concurrency, "worker_concurrency": f.policy.Concurrency, "p95_us": latencies[(len(latencies)*95)/100], "p99_us": latencies[(len(latencies)*99)/100], "drain_ms": time.Since(drainStarted).Milliseconds(), "reconciled": completed.Load(), "samples": samples, "scope": scope.ScopeRef{TenantID: f.tenant, Kind: "tenant", ID: f.tenant}}
	directory := os.Getenv("PANGOLIN_M4_METRICS_DIR")
	if directory == "" {
		directory = t.TempDir()
	}
	must(t, os.MkdirAll(directory, 0755))
	raw, err := json.MarshalIndent(report, "", "  ")
	must(t, err)
	must(t, os.WriteFile(filepath.Join(directory, "operations.json"), raw, 0600))
	t.Logf("records=%d concurrency=%d completed=%d samples=%d elapsed_ms=%d", records, concurrency, completed.Load(), len(samples), time.Since(began).Milliseconds())
}
