package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
)

type fakeIdempotency struct {
	records map[string][]byte
	calls   int
	err     error
}

func (f *fakeIdempotency) Execute(ctx context.Context, op, key string, body []byte, work func(context.Context) ([]byte, error)) ([]byte, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	k := op + ":" + key + ":" + string(body)
	if cached, ok := f.records[k]; ok {
		return append([]byte(nil), cached...), true, nil
	}
	f.calls++
	raw, err := work(ctx)
	if err != nil {
		return nil, false, err
	}
	if f.records == nil {
		f.records = map[string][]byte{}
	}
	f.records[k] = append([]byte(nil), raw...)
	return raw, false, nil
}

func boundAppContext(t *testing.T) context.Context {
	t.Helper()
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	ctx, err := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func newTestManager(t *testing.T) (uow.UnitOfWorkManager, *authorizedResource) {
	t.Helper()
	res := &authorizedResource{}
	m, err := uow.NewManager(res, uow.DefaultOptions{
		DefaultTimeout:      time.Second,
		CleanupTimeout:      time.Second,
		HookTimeout:         time.Second,
		MaxRequiresNewDepth: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return m, res
}

func TestAuthorizedGenericHelpers(t *testing.T) {
	ctx := boundAppContext(t)
	m, res := newTestManager(t)
	allow := authorizer(func(context.Context, string) error { return nil })
	deny := authorizer(func(context.Context, string) error { return scope.ErrDenied })

	// Fail closed on nil args.
	if _, err := AuthorizedCommandResult[int](nil, m, allow, "books.write", nil, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}
	if _, err := AuthorizedCommandResult[int](ctx, nil, allow, "books.write", nil, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}
	if _, err := AuthorizedCommandResult[int](ctx, m, allow, "books.write", nil, nil); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}
	if err := AuthorizedQuery(ctx, m, allow, "books.read", nil, nil); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}
	if _, err := AuthorizedQueryResult[int](ctx, nil, allow, "books.read", nil, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}

	// Entry denial happens before opening UOW.
	if _, err := AuthorizedCommandResult(ctx, m, deny, "books.write", nil, func(context.Context) (int, error) { return 7, nil }); !errors.Is(err, scope.ErrDenied) || res.begins != 0 {
		t.Fatalf("command entry denial failed: err=%v begins=%d", err, res.begins)
	}
	if _, err := AuthorizedQueryResult(ctx, m, deny, "books.read", nil, func(context.Context) (int, error) { return 7, nil }); !errors.Is(err, scope.ErrDenied) || res.begins != 0 {
		t.Fatalf("query entry denial failed: err=%v begins=%d", err, res.begins)
	}

	// Success paths.
	gotCmd, err := AuthorizedCommandResult(ctx, m, allow, "books.write", func(c context.Context) error {
		return uow.Check(c, true)
	}, func(context.Context) (string, error) { return "created", nil })
	if err != nil || gotCmd != "created" {
		t.Fatalf("AuthorizedCommandResult = %q, %v", gotCmd, err)
	}

	if err := AuthorizedQuery(ctx, m, allow, "books.read", func(c context.Context) error {
		return uow.Check(c, false)
	}, func(c context.Context) error {
		s, ok := uow.Current(c)
		if !ok || s.Options().Mode != uow.ReadOnly {
			return errors.New("query must not be writable")
		}
		return nil
	}); err != nil {
		t.Fatalf("AuthorizedQuery = %v", err)
	}

	gotQry, err := AuthorizedQueryResult(ctx, m, allow, "books.read", func(c context.Context) error {
		return scope.ErrDenied
	}, func(context.Context) (string, error) { return "should-not-run", nil })
	if !errors.Is(err, scope.ErrDenied) || gotQry != "" {
		t.Fatalf("resource denial leaked result: %q %v", gotQry, err)
	}
}

func TestRunnerCommandAndQuery(t *testing.T) {
	ctx := boundAppContext(t)
	m, res := newTestManager(t)
	allow := authorizer(func(context.Context, string) error { return nil })
	deny := authorizer(func(context.Context, string) error { return scope.ErrDenied })

	if _, err := NewRunner(nil, allow, nil); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("nil manager accepted: %v", err)
	}

	// Fail closed when Permission set but Authorizer is nil.
	rNoAuth, err := NewRunner(m, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunCommand(ctx, rNoAuth, CommandSpec{Permission: "books.write"}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, scope.ErrDenied) || res.begins != 0 {
		t.Fatalf("missing authorizer accepted on command: %v begins=%d", err, res.begins)
	}
	if _, err := RunQuery(ctx, rNoAuth, QuerySpec{Permission: "books.read"}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, scope.ErrDenied) || res.begins != 0 {
		t.Fatalf("missing authorizer accepted on query: %v begins=%d", err, res.begins)
	}
	// ResourcePolicy without Permission fails closed.
	if _, err := RunCommand(ctx, rNoAuth, CommandSpec{ResourcePolicy: func(context.Context) error { return nil }}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("resource policy without permission accepted: %v", err)
	}
	if _, err := RunQuery(ctx, rNoAuth, QuerySpec{ResourcePolicy: func(context.Context) error { return nil }}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("query resource policy without permission accepted: %v", err)
	}

	// Fail closed when Idempotency configured but Runner.Idempotency is nil or typed-nil.
	var typedNilIdem *fakeIdempotency
	rNoIdem, _ := NewRunner(m, allow, typedNilIdem)
	if _, err := RunCommand(ctx, rNoIdem, CommandSpec{Permission: "books.write", IdempotencyOp: "books.write", IdempotencyKey: "k1"}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("typed-nil idempotency accepted: %v", err)
	}

	// Incomplete idempotency spec (only key or only op) fails closed.
	idem := &fakeIdempotency{}
	r, err := NewRunner(m, allow, idem)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunCommand(ctx, r, CommandSpec{IdempotencyKey: "k1"}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("missing IdempotencyOp accepted: %v", err)
	}
	if _, err := RunCommand(ctx, r, CommandSpec{IdempotencyOp: "books.write"}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("missing IdempotencyKey accepted: %v", err)
	}

	// Incompatible UOW option overrides fail closed.
	if _, err := RunCommand(ctx, r, CommandSpec{Options: &uow.Options{Mode: uow.ReadOnly}}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("read-only command option accepted: %v", err)
	}
	if _, err := RunQuery(ctx, r, QuerySpec{Options: &uow.Options{Mode: uow.ReadWrite}}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("read-write query option accepted: %v", err)
	}

	// Nil runner / ctx / work fail closed.
	if _, err := RunCommand[int](ctx, nil, CommandSpec{}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}
	if _, err := RunQuery[int](ctx, nil, QuerySpec{}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}

	// Idempotent command execution + JSON replay.
	type bookDTO struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	fp, _ := json.Marshal(bookDTO{ID: "b1", Title: "Go"})
	spec := CommandSpec{
		Permission:     "books.write",
		IdempotencyOp:  "books.write",
		IdempotencyKey: "k1",
		Fingerprint:    fp,
		Options:        &uow.Options{Timeout: 500 * time.Millisecond},
	}
	runs := 0
	first, err := RunCommand(ctx, r, spec, func(c context.Context) (bookDTO, error) {
		if err := uow.Check(c, true); err != nil {
			return bookDTO{}, err
		}
		runs++
		return bookDTO{ID: "b1", Title: "Go"}, nil
	})
	if err != nil || first.ID != "b1" || runs != 1 {
		t.Fatalf("first run = %+v err=%v runs=%d", first, err, runs)
	}
	replayed, err := RunCommand(ctx, r, spec, func(context.Context) (bookDTO, error) {
		runs++
		return bookDTO{ID: "should-not-run"}, nil
	})
	if err != nil || replayed != first || runs != 1 {
		t.Fatalf("replay = %+v err=%v runs=%d", replayed, err, runs)
	}

	// Default empty fingerprint still succeeds.
	val, err := RunCommand(ctx, r, CommandSpec{IdempotencyOp: "ping", IdempotencyKey: "p1"}, func(context.Context) (string, error) {
		return "pong", nil
	})
	if err != nil || val != "pong" {
		t.Fatalf("empty fingerprint failed: %q %v", val, err)
	}

	// Non-idempotent command + query with resource policies.
	rDenied, _ := NewRunner(m, deny, idem)
	if _, err := RunCommand(ctx, rDenied, CommandSpec{Permission: "books.write"}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if _, err := RunQuery(ctx, rDenied, QuerySpec{Permission: "books.read"}, func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}

	qVal, err := RunQuery(ctx, r, QuerySpec{
		Permission:     "books.read",
		ResourcePolicy: func(c context.Context) error { return uow.Check(c, false) },
		Options:        &uow.Options{Timeout: 500 * time.Millisecond},
	}, func(context.Context) (string, error) {
		return "found", nil
	})
	if err != nil || qVal != "found" {
		t.Fatalf("RunQuery = %q, %v", qVal, err)
	}
}
