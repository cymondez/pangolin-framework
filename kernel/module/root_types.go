// Root Type references and optional module metadata contracts for root loading.
// The loader itself lives in root_loader.go; hook interfaces and root runtime
// wiring are owned by kernel/runtime and are intentionally unknown here.
package module

import "reflect"

// Ref references another module by Go type with an optional version constraint.
// An empty constraint is unconstrained: the edge accepts the resolved module
// version, which is still validated as SemVer.
type Ref interface {
	Type() reflect.Type
	VersionConstraint() string
}

// Type references module T. T must be a named struct type: use
// Of[AuthModule], never Of[*AuthModule]; LoadRoot rejects pointer,
// interface and unnamed struct references.
type Type[T any] struct {
	constraint string
}

// Of returns an unconstrained Type reference to module T.
func Of[T any]() Type[T] { return Type[T]{} }

// Requires returns a copy of the reference with an explicit version constraint.
// The constraint must satisfy the registry grammar ("=0.1.0" or ">=0.1.0 <0.2.0")
// and be satisfied by the target module's resolved Version.
func (t Type[T]) Requires(constraint string) Type[T] {
	return Type[T]{constraint: constraint}
}

// Type implements Ref and reports T itself, never a pointer type.
func (t Type[T]) Type() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

// VersionConstraint implements Ref.
func (t Type[T]) VersionConstraint() string { return t.constraint }

// Metadata is the optional base metadata a module declares through
// MetadataProvider. Empty fields fall back to defaults (derived qualified type
// name and Version "0.1.0", no required scope kinds), so one Go package can
// host several modules with distinct names.
type Metadata struct {
	Name               string
	Version            string
	RequiredScopeKinds []string
}

// MetadataProvider is the optional interface a module instance implements to
// override the derived base metadata.
type MetadataProvider interface {
	ModuleMetadata() Metadata
}

// Base is the ergonomic embeddable metadata descriptor. Embed it as a field
// so its ModuleMetadata method promotes to the module instance:
//
//	type AuthModule struct {
//	    module.Base
//	}
//
// All fields are optional: empty Name/Version keep the loader's derived
// defaults (full package path + type name, and Version "0.1.0").
type Base struct {
	Name               string
	Version            string
	RequiredScopeKinds []string
}

// ModuleMetadata makes embedding Base satisfy MetadataProvider, returning a
// defensive copy so mutation cannot affect the module's declared metadata.
func (b Base) ModuleMetadata() Metadata {
	return Metadata{
		Name:               b.Name,
		Version:            b.Version,
		RequiredScopeKinds: append([]string(nil), b.RequiredScopeKinds...),
	}
}

// Depender is the optional interface a module instance implements to declare
// Type dependencies: func (m *XModule) DependsOn() []module.Ref.
type Depender interface {
	DependsOn() []Ref
}
