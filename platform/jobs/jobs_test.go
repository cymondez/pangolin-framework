package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/security/scope"
)

func TestRegistry(t *testing.T) {
	definition := Definition{Type: "mail.send", Version: 1, Validate: func(p json.RawMessage) error {
		var v struct{ Message string }
		if json.Unmarshal(p, &v) != nil || v.Message == "" {
			return ErrInvalid
		}
		return nil
	}, Handle: func(context.Context, Job) error { return nil }}
	registry, err := NewRegistry(256, definition)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "j1", Type: definition.Type, Version: 1, Owner: scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"}, Actor: scope.PrincipalRef{Kind: "user", ID: "alice"}, Payload: json.RawMessage(`{"Message":"hello"}`), DueAt: time.Now(), Deadline: time.Now().Add(time.Minute)}
	if err = registry.Validate(job); err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*Job){func(j *Job) { j.Version = 2 }, func(j *Job) { j.Payload = []byte(`{}`) }, func(j *Job) { j.Payload = make([]byte, 257) }, func(j *Job) { j.Owner.ID = "b" }, func(j *Job) { j.Deadline = j.DueAt }, func(j *Job) { j.Actor.Kind = "untrusted" }, func(j *Job) { j.ID = "" }} {
		copy := job
		alter(&copy)
		if registry.Validate(copy) == nil {
			t.Fatal("invalid job accepted")
		}
	}
	if _, err = NewRegistry(0, definition); err == nil {
		t.Fatal("zero payload limit")
	}
	if _, err = NewRegistry(1, definition, definition); err == nil {
		t.Fatal("duplicate")
	}
	if _, err = NewRegistry(1, Definition{}); err == nil {
		t.Fatal("incomplete definition")
	}
	if _, err = NewRegistry(1); err == nil {
		t.Fatal("empty registry")
	}
}

func TestPolicy(t *testing.T) {
	policy := Policy{Lease: time.Second, ExecutionTimeout: 500 * time.Millisecond, CleanupTimeout: 100 * time.Millisecond, PollInterval: time.Millisecond, RetryInitial: time.Millisecond, RetryMax: 10 * time.Millisecond, MaxAttempts: 3, Concurrency: 2, BatchSize: 2, MaxPending: 10, Retention: time.Hour, JitterFraction: .2}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 100; i++ {
		if d := policy.Backoff(i); d <= 0 || d > policy.RetryMax {
			t.Fatal(d)
		}
	}
	bad := policy
	bad.Lease = bad.ExecutionTimeout
	if bad.Validate() == nil {
		t.Fatal("lease shorter than execution")
	}
	bad = policy
	bad.Concurrency = 0
	if bad.Validate() == nil {
		t.Fatal("unbounded concurrency")
	}
	bad = policy
	bad.JitterFraction = 2
	if bad.Validate() == nil {
		t.Fatal("jitter")
	}
}
