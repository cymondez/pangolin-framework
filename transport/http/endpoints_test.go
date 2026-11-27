package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/runtime"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type testCreateReq struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (r testCreateReq) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Title) == "" {
		return errors.New("id and title required")
	}
	return nil
}

type ptrValidateReq struct {
	Name string `json:"name"`
}

func (r *ptrValidateReq) Validate() error {
	if r.Name == "" {
		return errors.New("name required")
	}
	return nil
}

type unmarshalableRes struct {
	Bad func()
}

type stubParticipant struct {
	quiesced  atomic.Int32
	drained   atomic.Int32
	cancelled atomic.Int32
	qErr      error
	dErr      error
}

func (s *stubParticipant) Quiesce(context.Context) error { s.quiesced.Add(1); return s.qErr }
func (s *stubParticipant) Drain(context.Context) error   { s.drained.Add(1); return s.dErr }
func (s *stubParticipant) Cancel()                       { s.cancelled.Add(1) }

func testEndpointFactory(t *testing.T, allowPerm bool, stages *[]string, mu *sync.Mutex) *Endpoints {
	t.Helper()
	record := func(s string) {
		if stages == nil {
			return
		}
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		*stages = append(*stages, s)
	}
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	m, err := uow.NewManager(&pipelineResource{}, uow.DefaultOptions{
		DefaultTimeout:      time.Second,
		CleanupTimeout:      time.Second,
		HookTimeout:         time.Second,
		MaxRequiresNewDepth: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := NewEndpoints(EndpointConfig{
		Timeout:      time.Second,
		MaxBodyBytes: 128,
		Manager:      m,
		AuthenticateCredential: func(ctx context.Context, cred identity.Credential) (context.Context, error) {
			record("auth")
			if cred.Value != "good" {
				return nil, identity.ErrUnauthenticated
			}
			return ctx, nil
		},
		Credentials: BearerCredentials(),
		ResolveTenant: func(ctx context.Context, _ *http.Request) (context.Context, error) {
			record("tenant")
			return ctx, nil
		},
		ResolveScope: func(ctx context.Context, _ *http.Request) (context.Context, error) {
			record("scope")
			return resolver.Bind(ctx, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
		},
		Authorize: func(_ context.Context, perm string) error {
			record("entry:" + perm)
			if !allowPerm {
				return scope.ErrDenied
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ep
}

func TestProblemError(t *testing.T) {
	cause := errors.New("db constraint")
	err := NewProblemError(http.StatusConflict, "CONFLICT", cause)
	if err.Error() != "CONFLICT" || !errors.Is(err, cause) {
		t.Fatalf("unexpected ProblemError: %v", err)
	}
	var pe *ProblemError
	if !errors.As(err, &pe) || pe.HTTPStatus() != 409 || pe.Code() != "CONFLICT" {
		t.Fatalf("ProblemError inspection failed: %+v", pe)
	}

	r := httptest.NewRequest(http.MethodPost, "/books", nil)
	w := httptest.NewRecorder()
	WriteError(w, r, err)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	var problem Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "CONFLICT" || strings.Contains(w.Body.String(), "db constraint") {
		t.Fatalf("problem leaked or wrong code: %+v", problem)
	}

	// Out-of-range status and empty code fail closed to 500 INTERNAL.
	fallback := NewProblemError(200, "  ", nil)
	var fb *ProblemError
	if !errors.As(fallback, &fb) || fb.HTTPStatus() != 500 || fb.Code() != "INTERNAL" {
		t.Fatalf("fallback failed: %+v", fb)
	}
	var nilPE *ProblemError
	if nilPE.Error() != "INTERNAL" || nilPE.Unwrap() != nil || nilPE.HTTPStatus() != 500 || nilPE.Code() != "INTERNAL" {
		t.Fatal("nil ProblemError receiver did not fail closed")
	}
}

func TestServerParticipantLifecycle(t *testing.T) {
	extra := &stubParticipant{}
	var typedNil runtime.Participant = (*stubParticipant)(nil)
	p := NewServerParticipant(nil, extra, nil, typedNil)
	if p.Work() == nil {
		t.Fatal("expected default WorkGroup")
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	wrapped := p.Wrap(mux)
	server := &http.Server{Handler: wrapped}
	p.Attach(server)

	doneReq := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/slow", nil)
		wrapped.ServeHTTP(rec, req)
		doneReq <- rec.Code
	}()
	<-entered

	if err := p.Quiesce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if extra.quiesced.Load() != 1 {
		t.Fatal("extra participant not quiesced")
	}

	// New request after Quiesce must be rejected.
	rejected := httptest.NewRecorder()
	wrapped.ServeHTTP(rejected, httptest.NewRequest(http.MethodGet, "/slow", nil))
	if rejected.Code != http.StatusInternalServerError {
		t.Fatalf("expected admission refusal after quiesce, got %d", rejected.Code)
	}

	// Drain with short timeout while slow request is active times out.
	shortCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.Drain(shortCtx); err == nil {
		t.Fatal("drain succeeded while request active")
	}
	if extra.drained.Load() != 1 {
		t.Fatal("extra participant not drained")
	}

	// Cancel stops in-flight operation and subsequent Drain succeeds.
	p.Cancel()
	if extra.cancelled.Load() != 1 {
		t.Fatal("extra participant not cancelled")
	}
	<-doneReq
	if err := p.Drain(context.Background()); err != nil {
		t.Fatalf("drain after cancel failed: %v", err)
	}

	// Nil next handler and nil ServerParticipant fail safely.
	nilNext := httptest.NewRecorder()
	p.Wrap(nil).ServeHTTP(nilNext, httptest.NewRequest(http.MethodGet, "/", nil))
	if nilNext.Code != http.StatusInternalServerError {
		t.Fatalf("nil handler status = %d", nilNext.Code)
	}
	var nilSP *ServerParticipant
	nilSP.Attach(server)
	nilSP.Add(extra)
	nilSP.Cancel()
	if nilSP.Work() != nil || nilSP.Quiesce(context.Background()) != nil || nilSP.Drain(context.Background()) != nil {
		t.Fatal("nil ServerParticipant misbehaved")
	}
}

func TestEndpointsConstructionValidation(t *testing.T) {
	m, _ := uow.NewManager(&pipelineResource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	validBase := EndpointConfig{
		Timeout:       time.Second,
		MaxBodyBytes:  1024,
		Manager:       m,
		Authenticate:  func(c context.Context, _ string) (context.Context, error) { return c, nil },
		ResolveTenant: func(c context.Context, _ *http.Request) (context.Context, error) { return c, nil },
		ResolveScope:  func(c context.Context, _ *http.Request) (context.Context, error) { return c, nil },
	}
	if _, err := NewEndpoints(EndpointConfig{}); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("empty config accepted: %v", err)
	}
	bothAuth := validBase
	bothAuth.AuthenticateCredential = func(c context.Context, _ identity.Credential) (context.Context, error) { return c, nil }
	if _, err := NewEndpoints(bothAuth); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("both authenticators accepted: %v", err)
	}
	epNoAuthz, err := NewEndpoints(validBase)
	if err != nil {
		t.Fatal(err)
	}
	cmdFn := func(context.Context, Input[testCreateReq]) (testCreateReq, error) { return testCreateReq{}, nil }
	qryFn := func(context.Context, QueryInput) (testCreateReq, error) { return testCreateReq{}, nil }

	// Fail closed: nil ep, nil handler, missing entry/permission, missing Authorize for WithPermission, invalid option values, command-only options on query.
	if _, err := CommandEndpoint[testCreateReq, testCreateReq](nil, cmdFn, WithEntry(func(context.Context) error { return nil })); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}
	if _, err := CommandEndpoint[testCreateReq, testCreateReq](epNoAuthz, nil, WithEntry(func(context.Context) error { return nil })); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}
	if _, err := CommandEndpoint[testCreateReq, testCreateReq](epNoAuthz, cmdFn); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal("missing entry policy accepted")
	}
	if _, err := CommandEndpoint[testCreateReq, testCreateReq](epNoAuthz, cmdFn, WithPermission("books.write")); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal("WithPermission without Authorize accepted")
	}
	for _, opt := range []EndpointOption{
		nil,
		WithPermission(""),
		WithEntry(nil),
		WithStatus(199),
		WithStatus(300),
		WithIdempotencyHeader(" ", true),
		WithResourcePolicy[testCreateReq](nil),
		WithResourceCheck(nil),
		WithTimeout(0),
		WithMaxBodyBytes(0),
	} {
		if _, err := CommandEndpoint[testCreateReq, testCreateReq](epNoAuthz, cmdFn, WithEntry(func(context.Context) error { return nil }), opt); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("invalid option accepted: %v", err)
		}
	}
	if _, err := QueryEndpoint[testCreateReq](epNoAuthz, nil, WithEntry(func(context.Context) error { return nil })); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}
	if _, err := QueryEndpoint[testCreateReq](epNoAuthz, qryFn, WithEntry(func(context.Context) error { return nil }), WithIdempotencyHeader("Idempotency-Key", true)); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal("query accepted WithIdempotencyHeader")
	}
	if _, err := QueryEndpoint[testCreateReq](epNoAuthz, qryFn, WithEntry(func(context.Context) error { return nil }), WithResourcePolicy(func(context.Context, testCreateReq) error { return nil })); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal("query accepted WithResourcePolicy")
	}
}

func TestCommandAndQueryEndpointsLifecycle(t *testing.T) {
	var stages []string
	var mu sync.Mutex
	ep := testEndpointFactory(t, true, &stages, &mu)

	cmdHandler, err := CommandEndpoint(ep, func(ctx context.Context, in Input[testCreateReq]) (testCreateReq, error) {
		if err := uow.Check(ctx, true); err != nil {
			return testCreateReq{}, err
		}
		mu.Lock()
		stages = append(stages, "work:"+in.IdempotencyKey)
		mu.Unlock()
		return in.Body, nil
	},
		WithPermission("books.write"),
		WithStatus(http.StatusCreated),
		WithIdempotencyHeader("Idempotency-Key", true),
		WithResourcePolicy(func(ctx context.Context, req testCreateReq) error {
			if err := uow.Check(ctx, true); err != nil {
				return err
			}
			mu.Lock()
			stages = append(stages, "resource:"+req.ID)
			mu.Unlock()
			if req.ID == "forbidden" {
				return scope.ErrDenied
			}
			return nil
		}),
		WithResourceCheck(func(_ context.Context, _ *http.Request) error {
			mu.Lock()
			stages = append(stages, "check")
			mu.Unlock()
			return nil
		}),
		WithTimeout(2*time.Second),
		WithMaxBodyBytes(64),
	)
	if err != nil {
		t.Fatal(err)
	}

	qryHandler, err := QueryEndpoint(ep, func(ctx context.Context, in QueryInput) (testCreateReq, error) {
		if err := uow.Check(ctx, false); err != nil {
			return testCreateReq{}, err
		}
		s, ok := uow.Current(ctx)
		if !ok || s.Options().Mode != uow.ReadOnly {
			return testCreateReq{}, errors.New("query must be read-only")
		}
		return testCreateReq{ID: in.Request.PathValue("id"), Title: "Go"}, nil
	}, WithPermission("books.read"), WithResourceCheck(func(ctx context.Context, r *http.Request) error {
		if r.PathValue("id") == "blocked" {
			return scope.ErrDenied
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /books", cmdHandler)
	mux.Handle("GET /books/{id}", qryHandler)

	// 1. Happy path Command -> 201 Created + strict stage order.
	req := httptest.NewRequest(http.MethodPost, "/books", strings.NewReader(`{"id":"b1","title":"Clean"}`))
	req.Header.Set("Authorization", "Bearer good")
	req.Header.Set("Idempotency-Key", "k1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	wantStages := "auth,tenant,scope,entry:books.write,resource:b1,check,work:k1"
	if got := strings.Join(stages, ","); got != wantStages {
		t.Fatalf("stages = %s, want %s", got, wantStages)
	}

	// 2. Strict JSON failures: unknown field, trailing tokens, empty body, missing idempotency key, Validate() 422, oversized body 413.
	cases := []struct {
		name, body, idem string
		wantStatus       int
		wantCode         string
	}{
		{"unknown-field", `{"id":"b1","title":"t","extra":1}`, "k1", 400, "MALFORMED_REQUEST"},
		{"trailing-token", `{"id":"b1","title":"t"}{}`, "k1", 400, "MALFORMED_REQUEST"},
		{"empty-body", ``, "k1", 400, "MALFORMED_REQUEST"},
		{"missing-idem", `{"id":"b1","title":"t"}`, "", 400, "MALFORMED_REQUEST"},
		{"validate-422", `{"id":"","title":"t"}`, "k1", 422, "VALIDATION_FAILED"},
		{"resource-denied", `{"id":"forbidden","title":"t"}`, "k1", 403, "PERMISSION_DENIED"},
		{"oversized", `{"id":"b1","title":"` + strings.Repeat("x", 100) + `"}`, "k1", 413, "REQUEST_TOO_LARGE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/books", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer good")
			if tc.idem != "" {
				r.Header.Set("Idempotency-Key", tc.idem)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			var p Problem
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.Code != tc.wantCode {
				t.Fatalf("problem = %+v err=%v", p, err)
			}
		})
	}

	// 3. Query happy path with PathValue + body rejection + resource denial.
	qReq := httptest.NewRequest(http.MethodGet, "/books/book-42", nil)
	qReq.Header.Set("Authorization", "Bearer good")
	qRec := httptest.NewRecorder()
	mux.ServeHTTP(qRec, qReq)
	if qRec.Code != http.StatusOK || !strings.Contains(qRec.Body.String(), `"id":"book-42"`) {
		t.Fatalf("query status=%d body=%s", qRec.Code, qRec.Body.String())
	}

	qWithBody := httptest.NewRequest(http.MethodGet, "/books/book-42", strings.NewReader(`{}`))
	qWithBody.Header.Set("Authorization", "Bearer good")
	qBodyRec := httptest.NewRecorder()
	mux.ServeHTTP(qBodyRec, qWithBody)
	if qBodyRec.Code != http.StatusBadRequest {
		t.Fatalf("query with body status=%d", qBodyRec.Code)
	}

	qDenied := httptest.NewRequest(http.MethodGet, "/books/blocked", nil)
	qDenied.Header.Set("Authorization", "Bearer good")
	qDeniedRec := httptest.NewRecorder()
	mux.ServeHTTP(qDeniedRec, qDenied)
	if qDeniedRec.Code != http.StatusForbidden {
		t.Fatalf("query denied status=%d", qDeniedRec.Code)
	}

	// 4. Concurrent invocations share the startup-built handler safely.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest(http.MethodPost, "/books", strings.NewReader(`{"id":"c1","title":"ok"}`))
			r.Header.Set("Authorization", "Bearer good")
			r.Header.Set("Idempotency-Key", "k-conc")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != http.StatusCreated {
				t.Errorf("concurrent status = %d", w.Code)
			}
		}()
	}
	wg.Wait()
}

func TestCommandEndpointPostCommitSerializationAndPointerValidate(t *testing.T) {
	ep := testEndpointFactory(t, true, nil, nil)

	// Pointer-receiver Validate() -> 422.
	ptrHandler, err := CommandEndpoint(ep, func(_ context.Context, in Input[ptrValidateReq]) (ptrValidateReq, error) {
		return in.Body, nil
	}, WithPermission("books.write"))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"name":""}`))
	r.Header.Set("Authorization", "Bearer good")
	w := httptest.NewRecorder()
	ptrHandler.ServeHTTP(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("pointer Validate status = %d body=%s", w.Code, w.Body.String())
	}

	// Post-commit serialization failure preserves outcome="committed".
	badMarshal, err := CommandEndpoint(ep, func(_ context.Context, _ Input[testCreateReq]) (unmarshalableRes, error) {
		return unmarshalableRes{Bad: func() {}}, nil
	}, WithPermission("books.write"))
	if err != nil {
		t.Fatal(err)
	}
	r2 := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"id":"1","title":"ok"}`))
	r2.Header.Set("Authorization", "Bearer good")
	w2 := httptest.NewRecorder()
	badMarshal.ServeHTTP(w2, r2)
	if w2.Code != http.StatusInternalServerError {
		t.Fatalf("marshal failure status = %d", w2.Code)
	}
	var p Problem
	if err := json.Unmarshal(w2.Body.Bytes(), &p); err != nil || p.Outcome != "committed" || p.OperationID == "" {
		t.Fatalf("post-commit problem = %+v err=%v", p, err)
	}

	// 204 No Content command & query.
	noContentCmd, err := CommandEndpoint(ep, func(_ context.Context, _ Input[testCreateReq]) (struct{}, error) {
		return struct{}{}, nil
	}, WithPermission("books.write"), WithStatus(http.StatusNoContent))
	if err != nil {
		t.Fatal(err)
	}
	r3 := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"id":"1","title":"ok"}`))
	r3.Header.Set("Authorization", "Bearer good")
	w3 := httptest.NewRecorder()
	noContentCmd.ServeHTTP(w3, r3)
	if w3.Code != http.StatusNoContent || w3.Body.Len() != 0 {
		t.Fatalf("204 command status=%d body=%q", w3.Code, w3.Body.String())
	}

	noContentQry, err := QueryEndpoint(ep, func(_ context.Context, _ QueryInput) (struct{}, error) {
		return struct{}{}, nil
	}, WithPermission("books.read"), WithStatus(http.StatusNoContent))
	if err != nil {
		t.Fatal(err)
	}
	r4 := httptest.NewRequest(http.MethodGet, "/items", nil)
	r4.Header.Set("Authorization", "Bearer good")
	w4 := httptest.NewRecorder()
	noContentQry.ServeHTTP(w4, r4)
	if w4.Code != http.StatusNoContent || w4.Body.Len() != 0 {
		t.Fatalf("204 query status=%d body=%q", w4.Code, w4.Body.String())
	}

	// Handler panic is recovered cleanly as 500 INTERNAL.
	panicCmd, err := CommandEndpoint(ep, func(_ context.Context, _ Input[testCreateReq]) (struct{}, error) {
		panic("boom")
	}, WithPermission("books.write"))
	if err != nil {
		t.Fatal(err)
	}
	r5 := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"id":"1","title":"ok"}`))
	r5.Header.Set("Authorization", "Bearer good")
	w5 := httptest.NewRecorder()
	panicCmd.ServeHTTP(w5, r5)
	if w5.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d", w5.Code)
	}
}

func TestEndpointsLegacyBearerAndDenials(t *testing.T) {
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	m, _ := uow.NewManager(&pipelineResource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	rejected := 0
	ep, err := NewEndpoints(EndpointConfig{
		Timeout:      time.Second,
		MaxBodyBytes: 64,
		Manager:      m,
		Authenticate: func(ctx context.Context, tok string) (context.Context, error) {
			if tok != "ok" {
				return nil, identity.ErrUnauthenticated
			}
			return ctx, nil
		},
		ResolveTenant: func(ctx context.Context, _ *http.Request) (context.Context, error) { return ctx, nil },
		ResolveScope: func(ctx context.Context, _ *http.Request) (context.Context, error) {
			return resolver.Bind(ctx, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
		},
		Rejected: func(context.Context, error) { rejected++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := QueryEndpoint(ep, func(context.Context, QueryInput) (map[string]string, error) {
		return map[string]string{"ok": "1"}, nil
	}, WithEntry(func(context.Context) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}

	// Missing auth header -> 401 + Rejected callback.
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w1.Code != http.StatusUnauthorized || rejected != 1 {
		t.Fatalf("missing auth: code=%d rejected=%d", w1.Code, rejected)
	}

	// Valid Bearer token -> 200.
	r2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	r2.Header.Set("Authorization", "Bearer ok")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("valid bearer code=%d body=%s", w2.Code, w2.Body.String())
	}

	// Body read error returns 400.
	r3 := httptest.NewRequest(http.MethodGet, "/x", errReader{})
	r3.Header.Set("Authorization", "Bearer ok")
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("read error code=%d", w3.Code)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
