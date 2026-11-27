package main

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/events/nats"
	"github.com/pangolin-framework/pangolin/events/outbox"
	eventpg "github.com/pangolin-framework/pangolin/events/postgres"
	"github.com/pangolin-framework/pangolin/kernel/di"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/modules/realtime"
	"github.com/pangolin-framework/pangolin/modules/realtime/jetstream"
	"github.com/pangolin-framework/pangolin/persistence/migration"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type demoSettings struct {
	appDSN, workerDSN, brokerURL, stream, profile, key string
}

func (s demoSettings) subject() string { return "pangolin.realtime." + s.stream }

type demoResources struct {
	publisher *outbox.Publisher
	units     uow.UnitOfWorkManager
	store     *eventpg.Store
	broker    *nats.Broker
}

var dispatchPolicy = outbox.Policy{
	Lease: 15 * time.Second, SendTimeout: 2 * time.Second, RetryInitial: 100 * time.Millisecond,
	RetryMax: 2 * time.Second, MaxAttempts: 8,
}

type demoFoundation struct {
	settings     demoSettings
	resources    *demoResources
	authority    *fixtureAuthority
	receiver     *jetstream.Receiver
	stopWorker   context.CancelFunc
	cancelWorker context.CancelFunc
	workerDone   <-chan struct{}
}

func (f *demoFoundation) Configure(b module.RootBuilder) error {
	if err := b.Services().Add(f.open, module.Singleton()); err != nil {
		return err
	}
	return b.Services().Export(module.Contract[*demoResources]())
}

func (f *demoFoundation) open() (*demoResources, di.Cleanup, error) {
	s := f.settings
	if s.appDSN == "" || s.workerDSN == "" || s.brokerURL == "" || strings.ContainsAny(s.stream, ".*> /\\") {
		return nil, nil, errors.New("set the demo App DSN, Worker DSN and NATS URL and use a simple stream name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	appPool, err := pgxpool.New(ctx, s.appDSN)
	if err != nil {
		return nil, nil, errors.New("invalid demo application DSN")
	}
	workerPool, err := pgxpool.New(ctx, s.workerDSN)
	if err != nil {
		appPool.Close()
		return nil, nil, errors.New("invalid demo worker DSN")
	}
	closePools := func() { appPool.Close(); workerPool.Close() }
	if err := errors.Join(appPool.Ping(ctx), workerPool.Ping(ctx)); err != nil {
		closePools()
		return nil, nil, errors.New("demo PostgreSQL connection failed")
	}
	broker, err := nats.ConnectLive(s.brokerURL, s.stream, s.subject())
	if err != nil {
		closePools()
		return nil, nil, errors.New("demo NATS connection failed")
	}
	cleanup := func(context.Context) error {
		broker.Close()
		closePools()
		return nil
	}
	store, err := eventpg.NewStore(workerPool, dispatchPolicy)
	if err != nil {
		return nil, cleanup, err
	}
	registry, err := distributed.NewRegistry(realtime.Schema())
	if err != nil {
		return nil, cleanup, err
	}
	publisher, err := outbox.NewPublisher(registry, store)
	if err != nil {
		return nil, cleanup, err
	}
	manager, err := uow.NewManager(&eventpg.Resource{Base: &postgres.Resource{Pool: appPool}, Publisher: publisher},
		uow.DefaultOptions{DefaultTimeout: 3 * time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	if err != nil {
		return nil, cleanup, err
	}
	f.resources = &demoResources{publisher, manager, store, broker}
	return f.resources, cleanup, nil
}

func (f *demoFoundation) startIngress(ctx context.Context, hub *realtime.Hub) (func(context.Context) error, error) {
	resources := f.resources
	if resources == nil {
		return nil, errors.New("demo transport resources not constructed")
	}
	worker, err := outbox.NewWorker(resources.store, jetstream.Route{Realtime: resources.broker}, dispatchPolicy,
		"realtime-demo", f.authority.authorizeEvent)
	if err != nil {
		return nil, err
	}
	receiver, err := jetstream.Start(ctx, resources.broker, hub, 1024)
	if err != nil {
		return nil, err
	}
	run, stop := context.WithCancel(context.Background())
	workParent, abort := context.WithCancel(context.Background())
	done := make(chan struct{})
	f.receiver, f.stopWorker, f.cancelWorker, f.workerDone = receiver, stop, abort, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for run.Err() == nil {
			work, cancel := context.WithTimeout(workParent, 3*time.Second)
			_, err := worker.Dispatch(work)
			cancel()
			if err != nil {
				slog.Warn("realtime demo outbox dispatch failed")
			}
			select {
			case <-run.Done():
			case <-ticker.C:
			}
		}
	}()
	return func(cleanup context.Context) error {
		f.Cancel()
		return errors.Join(f.Drain(cleanup), receiver.Close(cleanup))
	}, nil
}

func (f *demoFoundation) Quiesce(ctx context.Context) error {
	if f.stopWorker != nil {
		f.stopWorker()
		return f.receiver.Quiesce(ctx)
	}
	return nil
}

func (f *demoFoundation) Drain(ctx context.Context) error {
	if f.workerDone == nil {
		return nil
	}
	select {
	case <-f.workerDone:
		return f.receiver.Drain(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *demoFoundation) Cancel() {
	if f.stopWorker != nil {
		f.stopWorker()
		f.cancelWorker()
		f.receiver.Cancel()
	}
}

func provision(ctx context.Context, s demoSettings, adminDSN string) error {
	if adminDSN == "" || s.brokerURL == "" {
		return errors.New("set the demo Admin DSN and NATS URL")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return errors.New("invalid demo admin DSN")
	}
	defer db.Close()
	source, err := fs.Sub(eventpg.Migrations, "migrations")
	if err != nil {
		return err
	}
	runner, err := migration.NewPostgres(db, source, 3200002, 3200002)
	if err != nil {
		return err
	}
	if err := runner.Apply(ctx, []migration.ModuleMigrations{{Module: "events", Directory: "."}}); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, demoSchema); err != nil {
		return err
	}
	broker, err := nats.Connect(s.brokerURL, s.stream, s.subject())
	if err != nil {
		return errors.New("demo NATS connection failed")
	}
	defer broker.Close()
	return broker.Provision(ctx)
}

const demoSchema = `
CREATE SCHEMA IF NOT EXISTS realtime_demo;
CREATE TABLE IF NOT EXISTS realtime_demo.notices(
	id text PRIMARY KEY, tenant_id text NOT NULL, scope_kind text NOT NULL, scope_id text NOT NULL
);
ALTER TABLE realtime_demo.notices ENABLE ROW LEVEL SECURITY;
ALTER TABLE realtime_demo.notices FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS owner ON realtime_demo.notices;
CREATE POLICY owner ON realtime_demo.notices TO pangolin_app USING (
	tenant_id = current_setting('app.tenant_id', true) AND
	scope_kind = current_setting('app.scope_kind', true) AND
	scope_id = current_setting('app.scope_id', true)
);
GRANT USAGE ON SCHEMA realtime_demo TO pangolin_app;
GRANT INSERT ON realtime_demo.notices TO pangolin_app;
`
