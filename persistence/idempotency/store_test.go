package idempotency

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

func TestConfigurationAndFingerprint(t *testing.T) {
	plain := func(b []byte) ([]byte, error) { return b, nil }
	valid := Config{MaxKeyBytes: 128, MaxBodyBytes: 1024, RetryWindow: time.Hour, Retention: time.Hour, Protect: plain, Unprotect: plain}
	allow := func(context.Context, string) error { return nil }
	for _, change := range []func(*Config){func(c *Config) { c.MaxKeyBytes = 0 }, func(c *Config) { c.MaxBodyBytes = 0 }, func(c *Config) { c.RetryWindow = 0 }, func(c *Config) { c.Retention = time.Minute }, func(c *Config) { c.Protect = nil }, func(c *Config) { c.Unprotect = nil }} {
		c := valid
		change(&c)
		if _, err := New(c, allow); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	if _, err := New(valid, nil); err == nil {
		t.Fatal("missing authorization")
	}
	first, err := CanonicalFingerprint([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalFingerprint([]byte(" {\n\"a\":1,\"b\":2} "))
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("object key order changed identity", err)
	}
	for _, body := range []string{"", `{"a":}`, `{} {}`, `{"a":1} trailing`} {
		if _, err := CanonicalFingerprint([]byte(body)); err == nil {
			t.Fatal(body)
		}
	}
	if equal([]byte{1}, []byte{1, 2}) || equal([]byte{1}, []byte{2}) || !equal([]byte{1}, []byte{1}) {
		t.Fatal("fingerprint comparison")
	}
	store, err := New(valid, allow)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.Execute(context.Background(), "op", "key", []byte(`{}`), nil); err == nil {
		t.Fatal("nil work")
	}
	if _, _, err = store.Execute(context.Background(), "op", "key", []byte(`{}`), func(context.Context) ([]byte, error) { t.Fatal("unbound work ran"); return nil, nil }); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if _, err = store.Query(context.Background(), "op", "key"); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{Kind: "user", ID: "alice"}, "tenant", true
	})
	ctx, err := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant", Kind: "tenant", ID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range []struct{ op, key, body string }{{"", "key", `{}`}, {"op", "", `{}`}, {"op", strings.Repeat("x", 129), `{}`}, {"op", "key", strings.Repeat("x", 1025)}, {"op", "key", `invalid`}} {
		if _, _, err = store.Execute(ctx, args.op, args.key, []byte(args.body), func(context.Context) ([]byte, error) { t.Fatal("invalid work ran"); return nil, nil }); err == nil {
			t.Fatal(args)
		}
	}
	denied, _ := New(valid, func(context.Context, string) error { return scope.ErrDenied })
	if _, _, err = denied.Execute(ctx, "op", "key", []byte(`{}`), func(context.Context) ([]byte, error) { t.Fatal("denied work ran"); return nil, nil }); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if _, err = store.Query(ctx, "op", "key"); err == nil {
		t.Fatal("query outside UOW")
	}
	if _, _, err = store.Execute(ctx, "op", "key", []byte(`{}`), func(context.Context) ([]byte, error) { t.Fatal("work outside UOW"); return nil, nil }); err == nil {
		t.Fatal("execute outside UOW")
	}
	valid.MaxBodyBytes = 1
	small, _ := New(valid, allow)
	if _, err = small.Query(ctx, "op", "key"); !errors.Is(err, uow.ErrRequired) {
		t.Fatal("bodyless result query was constrained by a synthetic body", err)
	}
}
