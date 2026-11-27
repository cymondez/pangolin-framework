package di

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type resolveTestCtx struct{}

func (*resolveTestCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*resolveTestCtx) Done() <-chan struct{}       { return nil }
func (*resolveTestCtx) Err() error                  { return nil }
func (*resolveTestCtx) Value(any) any               { return nil }

type stubKeyResolver struct {
	val any
	err error
}

func (r *stubKeyResolver) Resolve(context.Context, Token) (any, error) {
	if r == nil {
		return nil, nil
	}
	return r.val, r.err
}

func TestResolveKey(t *testing.T) {
	ctx := context.Background()
	key := NewKey[string]("app.greeting")

	// Success with real container.
	reg, err := NewRegistry(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Provide(Provider{
		Key:      key,
		Lifetime: Singleton,
		Factory:  func(context.Context, Resolver) (any, Cleanup, error) { return "hello", nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	container, err := reg.Seal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer container.Close(ctx)

	got, err := ResolveKey(ctx, container, key)
	if err != nil || got != "hello" {
		t.Fatalf("ResolveKey = %q, %v", got, err)
	}

	// Typed-nil resolved value matches pointer T.
	type svc struct{}
	ptrKey := NewKey[*svc]("app.svc")
	gotPtr, err := ResolveKey(ctx, &stubKeyResolver{val: (*svc)(nil)}, ptrKey)
	if err != nil || gotPtr != nil {
		t.Fatalf("typed-nil value = %v, %v", gotPtr, err)
	}

	// Wrong runtime type fails with clear error.
	if _, err := ResolveKey(ctx, &stubKeyResolver{val: 42}, key); err == nil || !strings.Contains(err.Error(), "expected type") {
		t.Fatalf("wrong type err = %v", err)
	}
	if _, err := ResolveKey(ctx, &stubKeyResolver{val: nil}, key); err == nil || !strings.Contains(err.Error(), "<nil>") {
		t.Fatalf("nil value err = %v", err)
	}

	// Resolver error preserves cause via errors.Is.
	cause := errors.New("not found")
	if _, err := ResolveKey(ctx, &stubKeyResolver{err: cause}, key); !errors.Is(err, cause) {
		t.Fatalf("cause not preserved: %v", err)
	}

	// Nil and typed-nil ctx/resolver and empty key fail closed.
	var typedNilCtx *resolveTestCtx
	var typedNilRes *stubKeyResolver
	for _, err := range []error{
		func() error { _, e := ResolveKey(nil, container, key); return e }(),
		func() error { _, e := ResolveKey(typedNilCtx, container, key); return e }(),
		func() error { _, e := ResolveKey(ctx, nil, key); return e }(),
		func() error { _, e := ResolveKey(ctx, typedNilRes, key); return e }(),
		func() error { _, e := ResolveKey(ctx, container, NewKey[string]("  ")); return e }(),
	} {
		if err == nil {
			t.Fatal("expected fail-closed error")
		}
	}
}
