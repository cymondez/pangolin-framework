// The realtime example uses disposable credentials and a loopback listener.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/app"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/modules/realtime"
	"github.com/pangolin-framework/pangolin/modules/realtime/signalr"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", "127.0.0.1:8088", "loopback listen address")
	profile := flag.String("profile", "tenant-only", "tenant-only, organization-enabled, or replacement-contract")
	stream := flag.String("stream", "PANGOLIN_REALTIME_DEMO", "dedicated JetStream stream")
	setup := flag.Bool("provision", false, "apply demo migrations and provision its JetStream stream")
	cleanupTarget := flag.String("cleanup-stream", "", "delete exactly this demo fixture stream and exit (NATS only)")
	stopOnEOF := flag.Bool("stop-on-stdin-eof", false, "stop when the test controller closes stdin")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if *stopOnEOF {
		go func() { _, _ = io.Copy(io.Discard, os.Stdin); cancel() }()
	}
	if *setup && *cleanupTarget != "" {
		return errors.New("--provision and --cleanup-stream are mutually exclusive")
	}
	if *cleanupTarget != "" {
		return cleanupStream(ctx, os.Getenv("PANGOLIN_REALTIME_NATS_URL"), *cleanupTarget)
	}
	settings := demoSettings{
		appDSN: os.Getenv("PANGOLIN_REALTIME_APP_DSN"), workerDSN: os.Getenv("PANGOLIN_REALTIME_WORKER_DSN"),
		brokerURL: os.Getenv("PANGOLIN_REALTIME_NATS_URL"), stream: *stream, profile: *profile,
		key: os.Getenv("PANGOLIN_REALTIME_DEMO_KEY"),
	}
	if *setup {
		return provision(ctx, settings, os.Getenv("PANGOLIN_REALTIME_ADMIN_DSN"))
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || !net.ParseIP(host).IsLoopback() || len(settings.key) < 16 {
		return errors.New("use a numeric loopback listen address and set PANGOLIN_REALTIME_DEMO_KEY to at least 16 characters")
	}
	authority, err := newFixture(settings)
	if err != nil {
		return err
	}
	root := &demoRoot{address: *listen}
	foundation := &demoFoundation{settings: settings, authority: authority}
	realtimeModule := &realtime.Module{
		Dependencies: []module.Ref{module.Of[demoFoundation]()},
		Constructor: func(db *demoResources) (*realtime.Hub, error) {
			return authority.hub(db.publisher, root)
		},
		StartIngress: foundation.startIngress,
	}
	return app.Run(ctx, root, app.Participants(root, foundation), app.Roots(foundation, realtimeModule))
}

type demoRoot struct {
	address  string
	api      *demoApplication
	server   *http.Server
	endpoint *signalr.Endpoint
}

func (*demoRoot) DependsOn() []module.Ref {
	return []module.Ref{module.Of[demoFoundation](), module.Of[realtime.Module]()}
}

func (*demoRoot) Configure(b module.RootBuilder) error {
	return b.Services().Add(newDemoApplication, module.Singleton())
}

func (r *demoRoot) Start(ctx context.Context, rt module.Runtime) error {
	service, err := rt.Services().Resolve(ctx, module.Contract[*demoApplication]())
	if err != nil {
		return err
	}
	r.api = service.(*demoApplication)
	gateway, err := rt.Services().Resolve(ctx, module.Contract[realtime.Gateway]())
	if err != nil {
		return err
	}
	r.endpoint, err = signalr.New(gateway.(realtime.Gateway), signalr.Options{
		Path: "/hub", NegotiationLimit: 128, HandshakeConcurrency: 16, AllowLoopbackCredentials: true,
		NegotiationTTL: 10 * time.Second, HandshakeTimeout: 5 * time.Second, ReadTimeout: 12 * time.Second,
		WriteTimeout: 2 * time.Second, KeepAlive: 2 * time.Second, MaxMessage: 64 << 10,
	})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", r.address)
	if err != nil {
		return err
	}
	r.server = &http.Server{Handler: r.endpoint, ReadHeaderTimeout: 3 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { _ = r.server.Serve(listener) }()
	fmt.Println("LISTEN http://" + listener.Addr().String() + "/hub")
	return nil
}

func (r *demoRoot) Quiesce(ctx context.Context) error {
	if r.endpoint != nil {
		return r.endpoint.Quiesce(ctx)
	}
	return nil
}

func (r *demoRoot) Drain(ctx context.Context) error {
	if r.server == nil {
		return nil
	}
	return errors.Join(r.endpoint.Drain(ctx), r.server.Shutdown(ctx))
}

func (r *demoRoot) Cancel() {
	if r.endpoint != nil {
		r.endpoint.Cancel()
	}
	if r.server != nil {
		_ = r.server.Close()
	}
}
