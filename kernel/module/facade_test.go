package module

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/di"
)

type facadeActual struct{ value string }

func (a *facadeActual) Value() string { return a.value }

type facadeWrong struct{}

type facadeContext struct{}

func (*facadeContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*facadeContext) Done() <-chan struct{}       { return nil }
func (*facadeContext) Err() error                  { return nil }
func (*facadeContext) Value(any) any               { return nil }

type facadeResolver struct {
	value any
	err   error
}

func (r *facadeResolver) Resolve(context.Context, di.Token) (any, error) {
	if r == nil {
		return nil, nil
	}
	return r.value, r.err
}

func TestResolveTypedActualWrongAndNil(t *testing.T) {
	ctx := context.Background()

	actual, err := Resolve[facadeActual](ctx, &facadeResolver{value: facadeActual{value: "ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if actual.value != "ok" {
		t.Fatalf("actual value = %q", actual.value)
	}

	typedNilResolved, err := Resolve[*facadeActual](ctx, &facadeResolver{value: (*facadeActual)(nil)})
	if err != nil {
		t.Fatalf("typed-nil resolved value error = %v", err)
	}
	if typedNilResolved != nil {
		t.Fatalf("typed-nil resolved value = %v", typedNilResolved)
	}

	_, err = Resolve[facadeActual](ctx, &facadeResolver{value: facadeWrong{}})
	if err == nil || !strings.Contains(err.Error(), "expected") || !strings.Contains(err.Error(), "module.facadeActual") || !strings.Contains(err.Error(), "module.facadeWrong") {
		t.Fatalf("wrong-type error = %v", err)
	}

	if _, err = Resolve[facadeActual](nil, &facadeResolver{value: facadeActual{}}); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil-context error = %v", err)
	}
	var typedNilContext *facadeContext
	if _, err = Resolve[facadeActual](typedNilContext, &facadeResolver{value: facadeActual{}}); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("typed-nil context error = %v", err)
	}
	if _, err = Resolve[facadeActual](ctx, nil); err == nil || !strings.Contains(err.Error(), "resolver") {
		t.Fatalf("nil-resolver error = %v", err)
	}
	var typedNil *facadeResolver
	if _, err = Resolve[facadeActual](ctx, typedNil); err == nil || !strings.Contains(err.Error(), "resolver") {
		t.Fatalf("typed-nil resolver error = %v", err)
	}
}

func TestAddFacadesCombineWithAsAndValidateLifetime(t *testing.T) {
	newComposition := func(t *testing.T) (Composition, Services) {
		t.Helper()
		composition, err := NewComposition(RootPlan{Plan: Plan{Ordered: []Module{{Name: "facade", Version: "0.1.0"}}}}, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		services, err := composition.Services("facade")
		if err != nil {
			t.Fatal(err)
		}
		return composition, services
	}

	t.Run("singleton plus As", func(t *testing.T) {
		_, services := newComposition(t)
		type facadeContract interface{ Value() string }
		if err := AddSingleton(services, func() *facadeActual { return &facadeActual{value: "singleton"} }, As(Contract[facadeContract]())); err != nil {
			t.Fatalf("facade plus As error = %v", err)
		}
	})

	t.Run("lifetime mutually exclusive", func(t *testing.T) {
		_, services := newComposition(t)
		if err := AddSingleton(services, func() *facadeActual { return &facadeActual{} }, Transient()); err == nil || !strings.Contains(err.Error(), "conflicting registration options") {
			t.Fatalf("conflicting lifetime error = %v", err)
		}
	})

	t.Run("three lifetime facades", func(t *testing.T) {
		_, services := newComposition(t)
		if err := AddTransient(services, func() facadeActual { return facadeActual{} }); err != nil {
			t.Fatal(err)
		}
		_, services = newComposition(t)
		if err := AddOperation(services, func() facadeActual { return facadeActual{} }); err != nil {
			t.Fatal(err)
		}
	})
}
