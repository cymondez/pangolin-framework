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

// AddAndExportSingleton registers constructor at singleton lifetime as Contract
// and exports Contract in one step.
func AddAndExportSingleton[ContractType any](services Services, constructor any, opts ...AddOption) error {
	return addAndExport[ContractType](services, Singleton(), "AddAndExportSingleton", constructor, opts...)
}

// AddAndExportTransient registers constructor at transient lifetime as Contract
// and exports Contract in one step.
func AddAndExportTransient[ContractType any](services Services, constructor any, opts ...AddOption) error {
	return addAndExport[ContractType](services, Transient(), "AddAndExportTransient", constructor, opts...)
}

// AddAndExportOperation registers constructor at operation lifetime as Contract
// and exports Contract in one step.
func AddAndExportOperation[ContractType any](services Services, constructor any, opts ...AddOption) error {
	return addAndExport[ContractType](services, Operation(), "AddAndExportOperation", constructor, opts...)
}

func addAndExport[ContractType any](services Services, lifetime AddOption, name string, constructor any, opts ...AddOption) error {
	if servicesIsNil(services) {
		return fmt.Errorf("module %s requires a non-nil Services registrar", name)
	}
	contract := Contract[ContractType]()
	combined := make([]AddOption, 0, len(opts)+2)
	combined = append(combined, lifetime, As(contract))
	combined = append(combined, opts...)
	if err := services.Add(constructor, combined...); err != nil {
		return err
	}
	return services.Export(contract)
}

// Registrar is an error-accumulating fluent helper over Services. Once any
// registration step returns an error, subsequent calls become no-ops and Err
// returns that first error.
type Registrar struct {
	services Services
	err      error
}

// Register creates a fluent Registrar over services. A nil or typed-nil
// services handle records an immediate error retrievable via Err.
func Register(services Services) *Registrar {
	if servicesIsNil(services) {
		return &Registrar{err: fmt.Errorf("module Register requires a non-nil Services registrar")}
	}
	return &Registrar{services: services}
}

// Singleton registers constructor at singleton lifetime.
func (r *Registrar) Singleton(constructor any, opts ...AddOption) *Registrar {
	if r == nil || r.err != nil {
		return r
	}
	r.err = AddSingleton(r.services, constructor, opts...)
	return r
}

// Transient registers constructor at transient lifetime.
func (r *Registrar) Transient(constructor any, opts ...AddOption) *Registrar {
	if r == nil || r.err != nil {
		return r
	}
	r.err = AddTransient(r.services, constructor, opts...)
	return r
}

// Operation registers constructor at operation lifetime.
func (r *Registrar) Operation(constructor any, opts ...AddOption) *Registrar {
	if r == nil || r.err != nil {
		return r
	}
	r.err = AddOperation(r.services, constructor, opts...)
	return r
}

// Export exports each token in order, stopping at the first error.
func (r *Registrar) Export(tokens ...di.Token) *Registrar {
	if r == nil || r.err != nil {
		return r
	}
	for _, token := range tokens {
		if err := r.services.Export(token); err != nil {
			r.err = err
			return r
		}
	}
	return r
}

// Bind selects impl for contract.
func (r *Registrar) Bind(contract di.Token, impl any) *Registrar {
	if r == nil || r.err != nil {
		return r
	}
	r.err = r.services.Bind(contract, impl)
	return r
}

// Replace replaces a registered constructor with a recorded reason.
func (r *Registrar) Replace(constructor any, reason string, opts ...AddOption) *Registrar {
	if r == nil || r.err != nil {
		return r
	}
	r.err = r.services.Replace(constructor, reason, opts...)
	return r
}

// ReplaceContract swaps an exported contract's implementation with a recorded reason.
func (r *Registrar) ReplaceContract(contract di.Token, constructor any, reason string, opts ...AddOption) *Registrar {
	if r == nil || r.err != nil {
		return r
	}
	r.err = r.services.ReplaceContract(contract, constructor, reason, opts...)
	return r
}

// ReplaceBinding swaps an existing contract binding with a recorded reason.
func (r *Registrar) ReplaceBinding(contract di.Token, impl any, reason string) *Registrar {
	if r == nil || r.err != nil {
		return r
	}
	r.err = r.services.ReplaceBinding(contract, impl, reason)
	return r
}

// Err returns the first error encountered by this Registrar, if any.
func (r *Registrar) Err() error {
	if r == nil {
		return fmt.Errorf("module Registrar is nil")
	}
	return r.err
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
