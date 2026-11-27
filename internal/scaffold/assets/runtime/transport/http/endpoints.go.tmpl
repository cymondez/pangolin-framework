package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/cymondez/pangolin-framework/application"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/identity"
)

// EndpointConfig holds shared transport dependencies configured once at startup
// and reused across every CommandEndpoint and QueryEndpoint without rebuilding
// pipelines per request.
type EndpointConfig struct {
	Timeout      time.Duration
	MaxBodyBytes int64
	Manager      uow.UnitOfWorkManager
	// Authenticate verifies a Bearer credential string. Exactly one of
	// Authenticate or AuthenticateCredential must be configured.
	Authenticate func(context.Context, string) (context.Context, error)
	// AuthenticateCredential verifies a full extracted credential.
	AuthenticateCredential func(context.Context, identity.Credential) (context.Context, error)
	// Credentials extracts a credential from the request (nil defaults to Bearer).
	Credentials   CredentialExtractor
	ResolveTenant func(context.Context, *http.Request) (context.Context, error)
	ResolveScope  func(context.Context, *http.Request) (context.Context, error)
	// Authorize checks permission strings configured via WithPermission.
	Authorize func(context.Context, string) error
	// Rejected records a safe, independent security event without altering outcome.
	Rejected func(context.Context, error)
}

// Endpoints is an immutable factory holder for typed HTTP command and query handlers.
type Endpoints struct {
	config EndpointConfig
}

// NewEndpoints validates cfg and returns a reusable endpoint factory.
func NewEndpoints(cfg EndpointConfig) (*Endpoints, error) {
	if cfg.Timeout <= 0 || cfg.MaxBodyBytes <= 0 || isNilInterface(cfg.Manager) ||
		(cfg.Authenticate == nil) == (cfg.AuthenticateCredential == nil) ||
		cfg.ResolveTenant == nil || cfg.ResolveScope == nil {
		return nil, uow.ErrOptionsInvalid
	}
	return &Endpoints{config: cfg}, nil
}

// Input delivers the strictly decoded JSON body, cloned request and optional
// extracted idempotency key to a CommandEndpoint handler.
type Input[Req any] struct {
	Body           Req
	Request        *http.Request
	IdempotencyKey string
}

// QueryInput delivers the cloned request to a QueryEndpoint handler.
type QueryInput struct {
	Request *http.Request
}

// EndpointOption configures an individual CommandEndpoint or QueryEndpoint.
type EndpointOption func(*endpointOptions) error

type endpointOptions struct {
	timeout             time.Duration
	maxBodyBytes        int64
	status              int
	permission          string
	entry               func(context.Context) error
	idempotencyHeader   string
	idempotencyRequired bool
	resourcePolicy      func(context.Context, any) error
	resourceCheck       func(context.Context, *http.Request) error
}

// WithPermission requires Authorize(ctx, permission) at entry before body
// decoding or UOW admission.
func WithPermission(permission string) EndpointOption {
	return func(o *endpointOptions) error {
		if strings.TrimSpace(permission) == "" {
			return uow.ErrOptionsInvalid
		}
		o.permission = permission
		return nil
	}
}

// WithEntry registers an explicit entry policy check run before decoding or UOW
// admission. When combined with WithPermission, both checks must pass.
func WithEntry(fn func(context.Context) error) EndpointOption {
	return func(o *endpointOptions) error {
		if fn == nil {
			return uow.ErrOptionsInvalid
		}
		o.entry = fn
		return nil
	}
}

// WithStatus overrides the default 200 OK success HTTP status code (must be 2xx).
func WithStatus(code int) EndpointOption {
	return func(o *endpointOptions) error {
		if code < 200 || code > 299 {
			return uow.ErrOptionsInvalid
		}
		o.status = code
		return nil
	}
}

// WithIdempotencyHeader reads header into Input.IdempotencyKey. When required is
// true, a missing or blank header fails with 400 MALFORMED_REQUEST before UOW admission.
func WithIdempotencyHeader(header string, required bool) EndpointOption {
	return func(o *endpointOptions) error {
		if strings.TrimSpace(header) == "" {
			return uow.ErrOptionsInvalid
		}
		o.idempotencyHeader = header
		o.idempotencyRequired = required
		return nil
	}
}

// WithResourcePolicy registers a typed resource policy executed inside the
// admitted command transaction with the decoded request body before the handler.
func WithResourcePolicy[Req any](fn func(context.Context, Req) error) EndpointOption {
	return func(o *endpointOptions) error {
		if fn == nil {
			return uow.ErrOptionsInvalid
		}
		o.resourcePolicy = func(ctx context.Context, raw any) error {
			req, ok := raw.(Req)
			if !ok {
				return uow.ErrOptionsInvalid
			}
			return fn(ctx, req)
		}
		return nil
	}
}

// WithResourceCheck registers a request-aware resource check executed inside
// the admitted unit of work before the handler runs.
func WithResourceCheck(fn func(context.Context, *http.Request) error) EndpointOption {
	return func(o *endpointOptions) error {
		if fn == nil {
			return uow.ErrOptionsInvalid
		}
		o.resourceCheck = fn
		return nil
	}
}

// WithTimeout overrides the base request timeout for one endpoint.
func WithTimeout(d time.Duration) EndpointOption {
	return func(o *endpointOptions) error {
		if d <= 0 {
			return uow.ErrOptionsInvalid
		}
		o.timeout = d
		return nil
	}
}

// WithMaxBodyBytes overrides the base maximum request body size for one endpoint.
func WithMaxBodyBytes(n int64) EndpointOption {
	return func(o *endpointOptions) error {
		if n <= 0 {
			return uow.ErrOptionsInvalid
		}
		o.maxBodyBytes = n
		return nil
	}
}

func (ep *Endpoints) buildOptions(query bool, opts []EndpointOption) (endpointOptions, error) {
	if ep == nil {
		return endpointOptions{}, uow.ErrOptionsInvalid
	}
	resolved := endpointOptions{
		timeout:      ep.config.Timeout,
		maxBodyBytes: ep.config.MaxBodyBytes,
		status:       http.StatusOK,
	}
	for _, opt := range opts {
		if opt == nil {
			return endpointOptions{}, uow.ErrOptionsInvalid
		}
		if err := opt(&resolved); err != nil {
			return endpointOptions{}, err
		}
	}
	if resolved.permission == "" && resolved.entry == nil {
		return endpointOptions{}, uow.ErrOptionsInvalid
	}
	if resolved.permission != "" && ep.config.Authorize == nil {
		return endpointOptions{}, uow.ErrOptionsInvalid
	}
	if query && (resolved.idempotencyHeader != "" || resolved.resourcePolicy != nil) {
		return endpointOptions{}, uow.ErrOptionsInvalid
	}
	return resolved, nil
}

// CommandEndpoint builds a startup-initialized, type-safe HTTP handler for a
// write command. It enforces the full security and transaction sequence:
// Trace/Timeout -> Auth -> Tenant -> Scope -> Entry Policy -> MaxBytes/Strict
// JSON Decode/Validate -> ReadWrite UOW -> Resource Policy -> Handler ->
// Confirmed Commit -> Serialize & 2xx response.
func CommandEndpoint[Req, Res any](ep *Endpoints, handler func(context.Context, Input[Req]) (Res, error), opts ...EndpointOption) (http.Handler, error) {
	if handler == nil {
		return nil, uow.ErrOptionsInvalid
	}
	resolved, err := ep.buildOptions(false, opts)
	if err != nil {
		return nil, err
	}
	cfg := ep.config
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel, cloned, fail, ok := ep.admit(w, r, resolved)
		if !ok {
			if cancel != nil {
				cancel()
			}
			return
		}
		defer cancel()
		defer func() {
			if recover() != nil {
				fail(errors.New("INTERNAL"))
			}
		}()

		raw, err := readBoundedBody(w, cloned, resolved.maxBodyBytes)
		if err != nil {
			fail(err)
			return
		}
		idemKey := ""
		if resolved.idempotencyHeader != "" {
			idemKey = cloned.Header.Get(resolved.idempotencyHeader)
			if resolved.idempotencyRequired && strings.TrimSpace(idemKey) == "" {
				fail(NewProblemError(http.StatusBadRequest, "MALFORMED_REQUEST", ErrValidation))
				return
			}
		}
		body, err := decodeStrictJSON[Req](raw)
		if err != nil {
			fail(err)
			return
		}
		if _, exists := cfg.Manager.Current(ctx); exists {
			fail(uow.ErrBusy)
			return
		}

		var result Res
		var opID string
		err = application.Command(ctx, cfg.Manager, func(c context.Context) error {
			if s, hasScope := cfg.Manager.Current(c); hasScope {
				opID = s.ID()
			}
			if resolved.resourcePolicy != nil {
				if err := resolved.resourcePolicy(c, body); err != nil {
					return err
				}
			}
			if resolved.resourceCheck != nil {
				if err := resolved.resourceCheck(c, cloned); err != nil {
					return err
				}
			}
			var workErr error
			result, workErr = handler(c, Input[Req]{Body: body, Request: cloned, IdempotencyKey: idemKey})
			return workErr
		})
		if err != nil {
			fail(err)
			return
		}
		if resolved.status == http.StatusNoContent {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		encoded, err := marshalResponse(result)
		if err != nil {
			writeProblem(w, cloned, http.StatusInternalServerError, "INTERNAL", opID, "committed")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resolved.status)
		if _, err = w.Write(encoded); err != nil {
			slog.Error("HTTP response failed after commit", "operation_id", opID, "outcome", "committed")
		}
	}), nil
}

// QueryEndpoint builds a startup-initialized, type-safe HTTP handler for a
// read-only query. Request bodies are rejected as 400 MALFORMED_REQUEST; the
// handler runs inside a ReadOnly unit of work and encodes its result only after
// the short read transaction finishes cleanly.
func QueryEndpoint[Res any](ep *Endpoints, handler func(context.Context, QueryInput) (Res, error), opts ...EndpointOption) (http.Handler, error) {
	if handler == nil {
		return nil, uow.ErrOptionsInvalid
	}
	resolved, err := ep.buildOptions(true, opts)
	if err != nil {
		return nil, err
	}
	cfg := ep.config
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel, cloned, fail, ok := ep.admit(w, r, resolved)
		if !ok {
			if cancel != nil {
				cancel()
			}
			return
		}
		defer cancel()
		defer func() {
			if recover() != nil {
				fail(errors.New("INTERNAL"))
			}
		}()

		raw, err := readBoundedBody(w, cloned, resolved.maxBodyBytes)
		if err != nil {
			fail(err)
			return
		}
		if len(raw) != 0 {
			fail(NewProblemError(http.StatusBadRequest, "MALFORMED_REQUEST", ErrValidation))
			return
		}

		var result Res
		err = cfg.Manager.Within(ctx, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error {
			if resolved.resourceCheck != nil {
				if err := resolved.resourceCheck(c, cloned); err != nil {
					return err
				}
			}
			var workErr error
			result, workErr = handler(c, QueryInput{Request: cloned})
			return workErr
		})
		if err != nil {
			fail(err)
			return
		}
		if resolved.status == http.StatusNoContent {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeResultWithStatus(w, cloned, resolved.status, result)
	}), nil
}

func (ep *Endpoints) admit(w http.ResponseWriter, r *http.Request, resolved endpointOptions) (ctx context.Context, cancel context.CancelFunc, cloned *http.Request, fail func(error), ok bool) {
	cfg := ep.config
	ctx, cancel = context.WithTimeout(r.Context(), resolved.timeout)
	cloned = r.Clone(ctx)
	cloned.Header.Set("X-Request-ID", rand.Text())
	fail = func(err error) {
		notifyRejected(cfg.Rejected, cloned.Context(), err)
		WriteError(w, cloned, err)
	}
	defer func() {
		if recover() != nil {
			ok = false
			fail(errors.New("INTERNAL"))
		}
	}()

	extract := cfg.Credentials
	if extract == nil {
		extract = BearerCredentials()
	}
	credential, present, extractErr := extract(cloned)
	if extractErr != nil {
		fail(extractErr)
		return nil, cancel, cloned, fail, false
	}
	if !present || credential.Value == "" {
		fail(identity.ErrUnauthenticated)
		return nil, cancel, cloned, fail, false
	}
	if cfg.AuthenticateCredential != nil {
		authenticated, err := cfg.AuthenticateCredential(ctx, credential)
		if err != nil {
			fail(err)
			return nil, cancel, cloned, fail, false
		}
		ctx = authenticated
	} else {
		if credential.Kind != identity.BearerKind {
			fail(identity.ErrUnauthenticated)
			return nil, cancel, cloned, fail, false
		}
		authenticated, err := cfg.Authenticate(ctx, credential.Value)
		if err != nil {
			fail(err)
			return nil, cancel, cloned, fail, false
		}
		ctx = authenticated
	}
	cloned = cloned.WithContext(ctx)
	var err error
	ctx, err = cfg.ResolveTenant(ctx, cloned)
	if err != nil {
		fail(err)
		return nil, cancel, cloned, fail, false
	}
	cloned = cloned.WithContext(ctx)
	ctx, err = cfg.ResolveScope(ctx, cloned)
	if err != nil {
		fail(err)
		return nil, cancel, cloned, fail, false
	}
	cloned = cloned.WithContext(ctx)
	if resolved.permission != "" {
		if err = cfg.Authorize(ctx, resolved.permission); err != nil {
			fail(err)
			return nil, cancel, cloned, fail, false
		}
	}
	if resolved.entry != nil {
		if err = resolved.entry(ctx); err != nil {
			fail(err)
			return nil, cancel, cloned, fail, false
		}
	}
	return ctx, cancel, cloned, fail, true
}

func readBoundedBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			return nil, ErrBodyTooLarge
		}
		return nil, ErrValidation
	}
	return raw, nil
}

func decodeStrictJSON[Req any](raw []byte) (Req, error) {
	var body Req
	if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
		return body, NewProblemError(http.StatusBadRequest, "MALFORMED_REQUEST", ErrValidation)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return body, NewProblemError(http.StatusBadRequest, "MALFORMED_REQUEST", errors.Join(ErrValidation, err))
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return body, NewProblemError(http.StatusBadRequest, "MALFORMED_REQUEST", ErrValidation)
	}
	if v, ok := any(body).(interface{ Validate() error }); ok {
		if !isNilInterface(v) {
			if err := v.Validate(); err != nil {
				return body, NewProblemError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", errors.Join(ErrValidation, err))
			}
		}
	} else if v, ok := any(&body).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return body, NewProblemError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", errors.Join(ErrValidation, err))
		}
	}
	return body, nil
}

func writeResultWithStatus(w http.ResponseWriter, r *http.Request, status int, result any) {
	raw, err := marshalResponse(result)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
