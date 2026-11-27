//go:build integration

package m6

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/examples/bookstore/application"
	"github.com/pangolin-framework/pangolin/examples/bookstore/infrastructure"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	loadOps         = 800
	loadConcurrency = 8
	loadWarmupOps   = 40
	loadPoolMax     = 16
)

// TestM6LoadBaseline runs the deterministic release load baseline against a
// unique tenant-only deployment: every operation is a real framework write —
// a ReadWrite UOW on the pangolin_app role under FORCE RLS performing the
// parameterized book insert plus the outbox insert of the reliable event.
// Raw per-operation latencies, pool saturation samples and heap deltas are
// saved for independent percentile recomputation; no throughput is asserted
// or promised, only measured.
func TestM6LoadBaseline(t *testing.T) {
	d := openDrillDatabase(t, "load")
	admin := d.adminPool(t)
	deployModules(t, d.sqlDB(t), false)
	installBookstoreSQL(t, admin, "schema.sql")
	serverVersion := d.serverVersion(t)

	poolConfig, err := pgxpool.ParseConfig(dsnWithDatabase(t, requireEnv(t, appDSNEnv), d.name))
	must(t, err)
	poolConfig.MaxConns = loadPoolMax
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	must(t, err)
	t.Cleanup(pool.Close)
	must(t, pool.Ping(context.Background()))

	manager := drillManager(t, pool)
	repository := infrastructure.Repository{}
	books := application.NewBooks(manager, repository, repository)
	ctx := bindTenantScope(t, "tenant-a", "load-user")

	// Warmup exercises the pools and caches before the measured window.
	warmupStarted := time.Now()
	warmupSamples := make([]int64, 0, loadWarmupOps)
	for i := 0; i < loadWarmupOps; i++ {
		started := time.Now()
		if _, err := books.Create(ctx, fmt.Sprintf("m6-warmup-%04d", i), "M6 load warmup"); err != nil {
			t.Fatal(err)
		}
		warmupSamples = append(warmupSamples, time.Since(started).Microseconds())
	}
	warmupSeconds := time.Since(warmupStarted).Seconds()

	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	start := make(chan struct{})
	failures := make(chan error, loadConcurrency)
	latencies := make([][]int64, loadConcurrency)
	var statistics sync.Mutex
	var poolSamples [][]map[string]int64
	loadStarted := time.Now()
	var wg sync.WaitGroup
	for worker := 0; worker < loadConcurrency; worker++ {
		worker := worker
		latencies[worker] = make([]int64, 0, loadOps/loadConcurrency)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			stats := make([]map[string]int64, 0, 4)
			for index := 0; index < loadOps/loadConcurrency; index++ {
				if index%25 == 0 {
					stat := pool.Stat()
					stats = append(stats, map[string]int64{"acquired": int64(stat.AcquiredConns()), "idle": int64(stat.IdleConns()), "total": int64(stat.TotalConns()), "max": int64(stat.MaxConns())})
				}
				started := time.Now()
				if _, err := books.Create(ctx, fmt.Sprintf("m6-load-%02d-%04d", worker, index), "M6 load baseline"); err != nil {
					failures <- err
					return
				}
				latencies[worker] = append(latencies[worker], time.Since(started).Microseconds())
			}
			statistics.Lock()
			poolSamples = append(poolSamples, stats)
			statistics.Unlock()
		}()
	}
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
	loadSeconds := time.Since(loadStarted).Seconds()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	samples := make([]int64, 0, loadOps)
	for _, workerSamples := range latencies {
		samples = append(samples, workerSamples...)
	}
	if len(samples) != loadOps {
		t.Fatalf("load samples lost: %d of %d", len(samples), loadOps)
	}
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	percentile := func(rank int) int64 { return sorted[(len(sorted)*rank)/100] }

	// Reconciliation on a fresh connection: every committed write and its
	// reliable outbox row must exist, without the worker running.
	var bookRows, outboxRows int64
	d.freshRow(t, "SELECT count(*) FROM pangolin.books WHERE tenant_id='tenant-a' AND id LIKE 'm6-%'", func(row pgx.Row) error { return row.Scan(&bookRows) })
	d.freshRow(t, "SELECT count(*) FROM pangolin.outbox WHERE state='pending'", func(row pgx.Row) error { return row.Scan(&outboxRows) })
	expectedRows := int64(loadWarmupOps + loadOps)
	if bookRows != expectedRows || outboxRows != expectedRows {
		t.Fatalf("load reconciliation failed: books=%d outbox=%d expected=%d", bookRows, outboxRows, expectedRows)
	}

	stat := pool.Stat()
	writeEvidence(t, "load-baseline.json", map[string]any{
		"database":       d.name,
		"server_version": serverVersion,
		"load": map[string]any{
			"ops":         loadOps,
			"concurrency": loadConcurrency,
			"warmup_ops":  loadWarmupOps,
			"mode":        "uow-ReadWrite parameterized INSERT business row + reliable outbox row (pangolin_app under FORCE RLS)",
		},
		"hardware": map[string]any{
			"goos":       runtime.GOOS,
			"goarch":     runtime.GOARCH,
			"num_cpu":    runtime.NumCPU(),
			"gomaxprocs": runtime.GOMAXPROCS(0),
			"go_version": runtime.Version(),
		},
		"database_pool": map[string]any{
			"role":      "pangolin_app",
			"max_conns": loadPoolMax,
			"samples":   poolSamples,
			"final":     map[string]int64{"acquired": int64(stat.AcquiredConns()), "idle": int64(stat.IdleConns()), "total": int64(stat.TotalConns()), "max": int64(stat.MaxConns())},
		},
		"percentiles": map[string]int64{
			"p50_us": percentile(50),
			"p95_us": percentile(95),
			"p99_us": percentile(99),
			"max_us": sorted[len(sorted)-1],
		},
		"samples_us":        samples,
		"warmup_samples_us": warmupSamples,
		"warmup_seconds":    warmupSeconds,
		"duration_seconds":  loadSeconds,
		"ops_per_second":    float64(loadOps) / loadSeconds,
		"reconciled": map[string]any{
			"books_rows":  bookRows,
			"outbox_rows": outboxRows,
			"errors":      0,
		},
		"heap": map[string]any{
			"alloc_bytes_start": before.HeapAlloc,
			"alloc_bytes_end":   after.HeapAlloc,
			"total_alloc_bytes": after.TotalAlloc,
			"num_gc":            after.NumGC - before.NumGC,
		},
		"method": "discrete percentiles using zero-based floor(count*rank/100) on sorted per-operation wall-clock microseconds; single tenant; every operation is one committed UOW transaction; one run with no confidence interval, affected by scheduler, network and timer precision",
	})
}
