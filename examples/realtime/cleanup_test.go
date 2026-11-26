package main

// Boundary tests for the fixture-cleanup path. The pure units cover name and
// subject verification; the live cases skip when no NATS URL is configured so
// the offline suite keeps passing.

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestValidateCleanupStream(t *testing.T) {
	for stream, want := range map[string]string{
		"":                       "exact",
		"RT_*":                   "wildcard",
		"RT_abc>":                "wildcard",
		"a.b":                    "wildcard",
		"a b":                    "wildcard",
		"a/b":                    "wildcard",
		`a\c`:                    "wildcard",
		"RT_0123456789abcdef":    "",
		"PANGOLIN_REALTIME_DEMO": "",
		"RT_cleanup_probe":       "",
	} {
		err := validateCleanupStream(stream)
		if (want == "") != (err == nil) {
			t.Errorf("validateCleanupStream(%q) error = %v, want failure=%v", stream, err, want != "")
			continue
		}
		if want != "" && !strings.Contains(err.Error(), want) {
			t.Errorf("validateCleanupStream(%q) error = %v, want it to mention %q", stream, err, want)
		}
	}
}

func TestVerifyDemoSubjects(t *testing.T) {
	if err := verifyDemoSubjects("RT_probe", []string{"pangolin.realtime.RT_probe"}); err != nil {
		t.Fatalf("subject verification failed: %v", err)
	}
	for _, subjects := range [][]string{nil, {}, {"other.subject"}, {"pangolin.realtime.RT_probe", "extra.subject"}} {
		if err := verifyDemoSubjects("RT_probe", subjects); err == nil {
			t.Fatalf("verification accepted subjects %v", subjects)
		}
	}
}

// liveJetStream connects when PANGOLIN_REALTIME_NATS_URL is configured,
// reports skips otherwise and never leaves fixture streams behind.
func liveJetStream(t *testing.T) nats.JetStreamContext {
	t.Helper()
	url := os.Getenv("PANGOLIN_REALTIME_NATS_URL")
	if url == "" {
		t.Skip("cleanup boundaries need PANGOLIN_REALTIME_NATS_URL")
	}
	connection, err := nats.Connect(url, nats.Timeout(3*time.Second))
	if err != nil {
		t.Skipf("live NATS not reachable, skipping cleanup boundaries: %v", err)
	}
	t.Cleanup(connection.Close)
	js, err := connection.JetStream(nats.MaxWait(5 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return js
}

func liveFixtureStream(t *testing.T, js nats.JetStreamContext, name string, config *nats.StreamConfig) {
	t.Helper()
	_ = js.DeleteStream(name, nats.MaxWait(5*time.Second))
	if _, err := js.AddStream(config, nats.MaxWait(5*time.Second)); err != nil {
		t.Fatalf("stream %q could not be created for the boundary test: %v", name, err)
	}
	t.Cleanup(func() { _ = js.DeleteStream(name, nats.MaxWait(5*time.Second)) })
}

func TestCleanupStreamLive(t *testing.T) {
	js := liveJetStream(t)
	url := os.Getenv("PANGOLIN_REALTIME_NATS_URL")

	probe := "RT_cleanup_probe"
	liveFixtureStream(t, js, probe, &nats.StreamConfig{
		Name: probe, Subjects: []string{demoSubject(probe)}, Storage: nats.MemoryStorage, MaxBytes: 1 << 20,
	})

	// Absent and mistyped routes must never delete anything and never fail the
	// harness on a second invocation.
	if err := cleanupStream(t.Context(), url, "RT_cleanup_absent_probe"); err != nil {
		t.Fatalf("absent stream cleanup is not idempotent: %v", err)
	}
	if err := cleanupStream(t.Context(), url, "RT_cleanup_*"); err == nil {
		t.Fatal("wildcard cleanup route was accepted")
	} else if strings.Contains(err.Error(), "inspect") {
		t.Fatalf("wildcard route reached the broker: %v", err)
	}

	if err := cleanupStream(t.Context(), url, probe); err != nil {
		t.Fatalf("owned stream cleanup failed: %v", err)
	}
	if _, err := js.StreamInfo(probe, nats.MaxWait(5*time.Second)); !errors.Is(err, nats.ErrStreamNotFound) {
		t.Fatalf("owned stream survived cleanup: %v", err)
	}
	if err := cleanupStream(t.Context(), url, probe); err != nil {
		t.Fatalf("repeated cleanup of a deleted stream failed: %v", err)
	}

	// A stream that does not carry exactly the demo subject is a foreign route
	// and must be refused entirely.
	impostor := "RT_cleanup_impostor"
	if _, err := js.AddStream(&nats.StreamConfig{
		Name: impostor, Subjects: []string{"foreign.subject"}, Storage: nats.MemoryStorage, MaxBytes: 1 << 20,
	}, nats.MaxWait(5*time.Second)); err != nil {
		t.Skipf("impostor stream could not be created: %v", err)
	}
	t.Cleanup(func() { _ = js.DeleteStream(impostor, nats.MaxWait(5*time.Second)) })
	if err := cleanupStream(t.Context(), url, impostor); err == nil {
		t.Fatal("foreign-route stream cleanup was accepted")
	} else if !strings.Contains(err.Error(), "do not match expected demo subject") {
		t.Fatalf("foreign-route refusal kept wrong cause: %v", err)
	}
	if _, err := js.StreamInfo(impostor, nats.MaxWait(5*time.Second)); err != nil {
		t.Fatalf("foreign-route stream was modified: %v", err)
	}
}
