// Fixture cleanup infrastructure for the realtime demo: on explicit request it
// deletes exactly one demo JetStream stream. It never inspects, purges or
// deletes any other stream and requires no database.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// demoSubject names the only subject the demo fixture ever owns for a stream.
func demoSubject(stream string) string { return "pangolin.realtime." + stream }

// validateCleanupStream refuses anything that is not an exact literal stream
// name, so that empty or wildcard-style input can never reach a delete call.
func validateCleanupStream(stream string) error {
	if stream == "" {
		return errors.New("cleanup requires an exact stream name")
	}
	if strings.ContainsAny(stream, ".*> /\\") {
		return fmt.Errorf("cleanup requires a simple stream name without wildcards: %q", stream)
	}
	return nil
}

// verifyDemoSubjects requires the stored stream configuration to declare
// exactly the demo subject of this stream, so a mistyped route can never
// delete an unrelated stream.
func verifyDemoSubjects(stream string, subjects []string) error {
	if len(subjects) != 1 || subjects[0] != demoSubject(stream) {
		return fmt.Errorf("stream %q subjects %v do not match expected demo subject %q", stream, subjects, demoSubject(stream))
	}
	return nil
}

// cleanupStream deletes the exact named demo fixture stream. An absent stream
// is success (idempotent), every other failure keeps its cause and the expected
// subjects gate the delete against a wrong route.
func cleanupStream(ctx context.Context, brokerURL, stream string) error {
	if err := validateCleanupStream(stream); err != nil {
		return err
	}
	if brokerURL == "" {
		return errors.New("set PANGOLIN_REALTIME_NATS_URL for cleanup")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	connection, err := nats.Connect(brokerURL, nats.Timeout(5*time.Second))
	if err != nil {
		return fmt.Errorf("demo NATS connection failed: %w", err)
	}
	defer connection.Close()
	js, err := connection.JetStream(nats.MaxWait(10 * time.Second))
	if err != nil {
		return fmt.Errorf("demo JetStream context failed: %w", err)
	}
	info, err := js.StreamInfo(stream, nats.Context(ctx))
	if errors.Is(err, nats.ErrStreamNotFound) {
		fmt.Println("CLEANUP ABSENT " + stream)
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect stream %q: %w", stream, err)
	}
	if info == nil {
		fmt.Println("CLEANUP ABSENT " + stream)
		return nil
	}
	if err := verifyDemoSubjects(stream, info.Config.Subjects); err != nil {
		return err
	}
	if err := js.DeleteStream(stream, nats.Context(ctx)); err != nil && !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("delete stream %q: %w", stream, err)
	}
	fmt.Println("CLEANED " + stream)
	return nil
}
