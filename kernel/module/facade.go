package module

import (
	"context"
	"fmt"
	"reflect"

	"github.com/cymondez/pangolin-framework/kernel/di"
)

// Resolve resolves T through the canonical module contract token and verifies
// the resolved value before returning it. Production hooks should prefer this
// error-returning façade over MustResolve.
func Resolve[T any](ctx context.Context, resolver di.Resolver) (T, error) {
	var zero T
	expected := reflect.TypeFor[T]()
	typeName := "<nil>"
	if expected != nil {
		typeName = expected.String()
	}
	if contextIsNil(ctx) {
		return zero, fmt.Errorf("module Resolve[%s]: context is nil", typeName)
	}
	if resolverIsNil(resolver) {
		return zero, fmt.Errorf("module Resolve[%s]: resolver is nil", typeName)
	}

	value, err := resolver.Resolve(ctx, Contract[T]())
	if err != nil {
		return zero, fmt.Errorf("module Resolve[%s]: resolve failed: %w", typeName, err)
	}
	resolved, ok := value.(T)
	if !ok {
		actualType := "<nil>"
		if value != nil {
			actualType = reflect.TypeOf(value).String()
		}
		return zero, fmt.Errorf("module Resolve[%s]: expected type %s, actual type %s", typeName, typeName, actualType)
	}
	return resolved, nil
}

// MustResolve is a panic-on-error convenience for fixtures and configuration.
// Production hooks should handle Resolve errors explicitly.
func MustResolve[T any](ctx context.Context, resolver di.Resolver) T {
	value, err := Resolve[T](ctx, resolver)
	if err != nil {
		panic(fmt.Sprintf("module MustResolve: %v", err))
	}
	return value
}

// AddSingleton registers constructor at singleton lifetime while preserving
// all existing Services.Add options such as As.
func AddSingleton(services Services, constructor any, opts ...AddOption) error {
	if servicesIsNil(services) {
		return fmt.Errorf("module AddSingleton requires a non-nil Services registrar")
	}
	combined := make([]AddOption, 0, len(opts)+1)
	combined = append(combined, Singleton())
	combined = append(combined, opts...)
	return services.Add(constructor, combined...)
}

// AddTransient registers constructor at transient lifetime while preserving
// all existing Services.Add options such as As.
func AddTransient(services Services, constructor any, opts ...AddOption) error {
	if servicesIsNil(services) {
		return fmt.Errorf("module AddTransient requires a non-nil Services registrar")
	}
	combined := make([]AddOption, 0, len(opts)+1)
	combined = append(combined, Transient())
	combined = append(combined, opts...)
	return services.Add(constructor, combined...)
}

// AddOperation registers constructor at operation lifetime while preserving
// all existing Services.Add options such as As.
func AddOperation(services Services, constructor any, opts ...AddOption) error {
	if servicesIsNil(services) {
		return fmt.Errorf("module AddOperation requires a non-nil Services registrar")
	}
	combined := make([]AddOption, 0, len(opts)+1)
	combined = append(combined, Operation())
	combined = append(combined, opts...)
	return services.Add(constructor, combined...)
}

func contextIsNil(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	value := reflect.ValueOf(ctx)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func resolverIsNil(resolver di.Resolver) bool {
	if resolver == nil {
		return true
	}
	value := reflect.ValueOf(resolver)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func servicesIsNil(services Services) bool {
	if services == nil {
		return true
	}
	value := reflect.ValueOf(services)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
