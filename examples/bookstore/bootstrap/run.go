// Package bootstrap composes the M1 API/worker executables with kernel lifecycle.
package bootstrap

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	broker "github.com/pangolin-framework/pangolin/events/nats"
	"github.com/pangolin-framework/pangolin/examples/bookstore/application"
	"github.com/pangolin-framework/pangolin/examples/bookstore/infrastructure"
	transport "github.com/pangolin-framework/pangolin/examples/bookstore/transport/http"
	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/di"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/authn"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	frameworkhttp "github.com/pangolin-framework/pangolin/transport/http"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

type httpParticipant struct {
	group  *runtime.WorkGroup
	server *http.Server
}

func (p *httpParticipant) Quiesce(ctx context.Context) error {
	p.server.SetKeepAlivesEnabled(false)
	return p.group.Quiesce(ctx)
}
func (p *httpParticipant) Drain(ctx context.Context) error {
	return errors.Join(p.server.Shutdown(ctx), p.group.Drain(ctx))
}
func (p *httpParticipant) Cancel() { p.group.Cancel() }

type workerParticipant struct {
	group  *runtime.WorkGroup
	cancel context.CancelFunc
}

func (p *workerParticipant) Quiesce(ctx context.Context) error {
	p.cancel()
	return p.group.Quiesce(ctx)
}
func (p *workerParticipant) Drain(ctx context.Context) error { return p.group.Drain(ctx) }
func (p *workerParticipant) Cancel()                         { p.group.Cancel() }

// Run keeps secrets in environment variables; file/CLI sources cannot supply DSNs.
// An optional publisher decorator is a fault-injection seam used only by testkit.
func Run(ctx context.Context, mode string, args []string, out io.Writer, decorate func(distributed.BrokerPublisher) distributed.BrokerPublisher) error {
	flags := flag.NewFlagSet("bookstore-"+mode, flag.ContinueOnError)
	flags.SetOutput(out)
	listen := flags.String("listen", "127.0.0.1:18081", "API listen address")
	runFor := flags.Duration("run-for", 0, "optional bounded demo lifetime")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *runFor < 0 {
		return fmt.Errorf("run-for cannot be negative")
	}
	if *runFor > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *runFor)
		defer cancel()
	}
	values := map[string]string{"database.dsn": os.Getenv("PANGOLIN_DSN"), "jwt.issuer": os.Getenv("PANGOLIN_ISSUER"), "jwt.audience": "pangolin-bookstore", "broker.url": os.Getenv("PANGOLIN_NATS_URL"), "worker.tenants": os.Getenv("PANGOLIN_WORKER_TENANTS")}
	values["jwt.jwks_url"] = os.Getenv("PANGOLIN_JWKS_URL")
	if values["jwt.jwks_url"] == "" {
		values["jwt.jwks_url"] = strings.TrimRight(values["jwt.issuer"], "/") + "/jwks.json"
	}
	rules := []config.Rule{{Key: "database.dsn", Required: true, Validate: config.URL("postgres", "postgresql")}}
	if mode == "api" {
		rules = append(rules, config.Rule{Key: "jwt.issuer", Required: true, Validate: config.URL("http", "https")}, config.Rule{Key: "jwt.jwks_url", Required: true, Validate: config.URL("http", "https")})
	} else if mode == "worker" {
		rules = append(rules, config.Rule{Key: "broker.url", Required: true, Validate: config.URL("nats", "tls")}, config.Rule{Key: "worker.tenants", Required: true})
	} else {
		return fmt.Errorf("unknown process mode")
	}
	if _, e := config.NewLoader(rules).Load([]config.Source{{Name: "environment", Values: values}}); e != nil {
		return e
	}
	group := runtime.NewWorkGroup()
	server := &http.Server{Addr: *listen, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 6 * time.Second, WriteTimeout: 6 * time.Second, IdleTimeout: 30 * time.Second}
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	var participant runtime.Participant = &httpParticipant{group, server}
	if mode == "worker" {
		participant = &workerParticipant{group, cancelWorker}
	}
	defaults := map[string]string{"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s", "uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4"}
	h, e := runtime.New(runtime.Options{DrainTimeout: 15 * time.Second, CleanupTimeout: 5 * time.Second}, []config.Source{{Name: "defaults", Values: defaults}, {Name: "environment", Values: values}}, participant)
	if e != nil {
		return e
	}
	poolKey := di.NewKey[*pgxpool.Pool]("postgres")
	brokerKey := di.NewKey[*broker.Broker]("broker")
	modules := []module.Module{{Name: "database", Version: "0.1.0", Configure: func(b module.Builder) error {
		return b.Provide(di.Provider{Key: poolKey, Lifetime: di.Singleton, Factory: func(ctx context.Context, _ di.Resolver) (any, di.Cleanup, error) {
			c, e := pgxpool.ParseConfig(values["database.dsn"])
			if e != nil {
				return nil, nil, fmt.Errorf("invalid database configuration")
			}
			c.MaxConns = 4
			pool, e := pgxpool.NewWithConfig(ctx, c)
			if e != nil {
				return nil, nil, e
			}
			return pool, func(context.Context) error { pool.Close(); return nil }, pool.Ping(ctx)
		}})
	}}}
	errorsChannel := make(chan error, 1)
	if mode == "api" {
		modules = append(modules, module.Module{Name: "api", Version: "0.1.0", DependsOn: []module.Dependency{{Name: "database", VersionConstraint: "=0.1.0"}}, Start: func(start context.Context, r module.Runtime) error {
			value, e := r.Services().Resolve(start, poolKey)
			if e != nil {
				return e
			}
			pool := value.(*pgxpool.Pool)
			if _, e = pool.Exec(start, "SELECT id FROM pangolin.books LIMIT 0"); e != nil {
				return fmt.Errorf("schema compatibility check failed: %w", e)
			}
			manager, e := uow.NewManager(&postgres.Resource{Pool: pool}, uow.DefaultOptions{DefaultTimeout: 5 * time.Second, CleanupTimeout: 2 * time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 4})
			if e != nil {
				return e
			}
			// The API entry uses the extension ports: Bearer credential
			// extraction plus the trusted credential authentication pipeline.
			// The JWT validator remains the default working profile.
			validator := authn.NewValidator(values["jwt.issuer"], values["jwt.audience"], values["jwt.jwks_url"], &http.Client{Timeout: 2 * time.Second})
			server.Handler = &transport.Handler{Books: application.NewBooks(manager, infrastructure.Repository{}, infrastructure.Repository{}), CredentialAuth: validator, Credentials: frameworkhttp.BearerCredentials(), Scopes: scope.NewTenantResolver(identity.Identity), Work: group, Ready: h.Ready}
			listener, e := net.Listen("tcp", *listen)
			if e != nil {
				return e
			}
			go func() {
				e := server.Serve(listener)
				if !errors.Is(e, http.ErrServerClosed) {
					select {
					case errorsChannel <- e:
					default:
					}
				}
			}()
			return nil
		}, Stop: func(context.Context, module.Runtime) error { return server.Close() }})
	} else {
		stream := os.Getenv("PANGOLIN_NATS_STREAM")
		if stream == "" {
			stream = "PANGOLIN_M1"
		}
		subject := os.Getenv("PANGOLIN_NATS_SUBJECT")
		if subject == "" {
			subject = "pangolin.m1.book_created"
		}
		modules = append(modules, module.Module{Name: "broker", Version: "0.1.0", Configure: func(b module.Builder) error {
			return b.Provide(di.Provider{Key: brokerKey, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
				client, e := broker.Connect(values["broker.url"], stream, subject)
				if e != nil {
					return nil, nil, e
				}
				return client, func(context.Context) error { client.Close(); return nil }, nil
			}})
		}})
		modules = append(modules, module.Module{Name: "worker", Version: "0.1.0", DependsOn: []module.Dependency{{Name: "database", VersionConstraint: "=0.1.0"}, {Name: "broker", VersionConstraint: "=0.1.0"}}, Start: func(start context.Context, r module.Runtime) error {
			db, e := r.Services().Resolve(start, poolKey)
			if e != nil {
				return e
			}
			b, e := r.Services().Resolve(start, brokerKey)
			if e != nil {
				return e
			}
			client := b.(*broker.Broker)
			subscription, e := client.Subscribe("bookstore")
			if e != nil {
				return e
			}
			tenants := map[string]bool{}
			for _, tenant := range strings.Split(values["worker.tenants"], ",") {
				if tenant != "" {
					tenants[tenant] = true
				}
			}
			var publisher distributed.BrokerPublisher = client
			if decorate != nil {
				publisher = decorate(client)
			}
			replicator := &infrastructure.Replicator{Pool: db.(*pgxpool.Pool), Broker: publisher, Tenants: tenants}
			go func() {
				for workerCtx.Err() == nil {
					work, done, e := group.Enter(context.WithoutCancel(ctx))
					if e != nil {
						return
					}
					operation, cancel := context.WithTimeout(work, 3*time.Second)
					_, e = replicator.Dispatch(operation)
					if e == nil {
						receiveCtx, stopReceive := context.WithTimeout(operation, 100*time.Millisecond)
						delivery, receiveErr := subscription.Next(receiveCtx)
						stopReceive()
						if receiveErr == nil {
							e = replicator.Receive(operation, delivery.Envelope)
							if e == nil {
								e = delivery.Ack(operation)
							}
						} else if !errors.Is(receiveErr, context.DeadlineExceeded) && !errors.Is(receiveErr, nats.ErrTimeout) {
							e = receiveErr
						}
					}
					if e == nil {
						_, e = replicator.Project(operation)
					}
					cancel()
					done()
					if e != nil && workerCtx.Err() == nil {
						select {
						case errorsChannel <- e:
						default:
						}
						return
					}
				}
			}()
			return nil
		}})
	}
	if e = h.Start(ctx, modules); e != nil {
		return e
	}
	_, _ = fmt.Fprintln(out, "ready", mode)
	select {
	case <-ctx.Done():
		e = nil
	case e = <-errorsChannel:
	}
	return errors.Join(e, h.Stop(context.WithoutCancel(ctx)))
}
