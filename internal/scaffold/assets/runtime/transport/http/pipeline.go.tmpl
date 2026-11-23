package http

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/authn"
)

var ErrValidation = errors.New("VALIDATION_FAILED")
var ErrBodyTooLarge = errors.New("REQUEST_TOO_LARGE")

type PipelineOptions struct {
	Timeout        time.Duration
	MaxBodyBytes   int64
	Manager        uow.UnitOfWorkManager
	ReadOnly       bool
	Authenticate   func(context.Context, string) (context.Context, error)
	ResolveTenant  func(context.Context, *http.Request) (context.Context, error)
	ResolveScope   func(context.Context, *http.Request) (context.Context, error)
	Entry          func(context.Context) error
	Decode         func([]byte) (any, error)
	ResourcePolicy func(context.Context, any) error
	Work           func(context.Context, any) (any, error)
	// Rejected records a safe, independent security event. It cannot alter outcome.
	Rejected func(context.Context, error)
}
type Pipeline struct{ options PipelineOptions }

func notifyRejected(f func(context.Context, error), ctx context.Context, err error) {
	if f != nil {
		func() { defer func() { _ = recover() }(); f(context.WithoutCancel(ctx), err) }()
	}
}

func NewPipeline(o PipelineOptions) (*Pipeline, error) {
	if o.Timeout <= 0 || o.MaxBodyBytes <= 0 || o.Manager == nil || o.Authenticate == nil || o.ResolveTenant == nil || o.ResolveScope == nil || o.Entry == nil || o.Decode == nil || o.Work == nil {
		return nil, uow.ErrOptionsInvalid
	}
	return &Pipeline{o}, nil
}
func (p *Pipeline) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o := p.options
	ctx, cancel := context.WithTimeout(r.Context(), o.Timeout)
	defer cancel()
	r = r.Clone(ctx)
	r.Header.Set("X-Request-ID", rand.Text())
	fail := func(err error) {
		if o.Rejected != nil {
			func() { defer func() { _ = recover() }(); o.Rejected(context.WithoutCancel(r.Context()), err) }()
		}
		WriteError(w, r, err)
	}
	defer func() {
		if recover() != nil {
			fail(errors.New("INTERNAL"))
		}
	}()
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || strings.ContainsAny(strings.TrimPrefix(auth, "Bearer "), " \t\r\n") || len(auth) <= 7 {
		fail(authn.ErrUnauthenticated)
		return
	}
	ctx, err := o.Authenticate(ctx, strings.TrimPrefix(auth, "Bearer "))
	if err != nil {
		fail(err)
		return
	}
	r = r.WithContext(ctx)
	ctx, err = o.ResolveTenant(ctx, r)
	if err != nil {
		fail(err)
		return
	}
	r = r.WithContext(ctx)
	ctx, err = o.ResolveScope(ctx, r)
	if err != nil {
		fail(err)
		return
	}
	r = r.WithContext(ctx)
	if err = o.Entry(ctx); err != nil {
		fail(err)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, o.MaxBodyBytes))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			err = ErrBodyTooLarge
		} else {
			err = ErrValidation
		}
		fail(err)
		return
	}
	dto, err := o.Decode(raw)
	if err != nil {
		fail(errors.Join(ErrValidation, err))
		return
	}
	work := func(c context.Context) (any, error) {
		if o.ResourcePolicy != nil {
			if err := o.ResourcePolicy(c, dto); err != nil {
				return nil, err
			}
		}
		return o.Work(c, dto)
	}
	if !o.ReadOnly {
		command(o.Manager, work, o.Rejected).ServeHTTP(w, r)
		return
	}
	var result any
	err = o.Manager.Within(ctx, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error { var e error; result, e = work(c); return e })
	if err != nil {
		fail(err)
		return
	}
	// Query result is encoded after its short read transaction completes.
	writeResult(w, r, result)
}
