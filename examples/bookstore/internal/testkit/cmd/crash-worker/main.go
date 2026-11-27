// Command crash-worker terminates after a real publish acknowledgement and
// before the outbox mark-sent update. It is exclusively a process fault fixture.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/examples/bookstore/bootstrap"
)

type crash struct{ publisher distributed.BrokerPublisher }

func (c crash) Send(ctx context.Context, event distributed.Envelope) error {
	if e := c.publisher.Send(ctx, event); e != nil {
		return e
	}
	fmt.Println("crash-after-ack", event.ID)
	os.Exit(17)
	return nil
}
func main() {
	if e := bootstrap.Run(context.Background(), "worker", os.Args[1:], os.Stdout, func(p distributed.BrokerPublisher) distributed.BrokerPublisher { return crash{p} }); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
