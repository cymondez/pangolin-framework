package di

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// ResolveKey resolves a typed Key[T] from resolver, validating ctx, resolver,
// key identity and the resolved value's runtime type before returning it.
func ResolveKey[T any](ctx context.Context, resolver Resolver, key Key[T]) (T, error) {
	var zero T
	expected := reflect.TypeFor[T]()
	typeName := "<nil>"
	if expected != nil {
		typeName = expected.String()
	}
	if isNilInterface(ctx) {
		return zero, fmt.Errorf("di ResolveKey[%s](%q): context is nil", typeName, key.Name())
	}
	if isNilInterface(resolver) {
		return zero, fmt.Errorf("di ResolveKey[%s](%q): resolver is nil", typeName, key.Name())
	}
	if strings.TrimSpace(key.Name()) == "" || key.Type() == nil {
		return zero, fmt.Errorf("di ResolveKey[%s]: invalid key %q", typeName, key.Name())
	}
	value, err := resolver.Resolve(ctx, key)
	if err != nil {
		return zero, fmt.Errorf("di ResolveKey[%s](%q): %w", typeName, key.Name(), err)
	}
	resolved, ok := value.(T)
	if !ok {
		actualType := "<nil>"
		if value != nil {
			actualType = reflect.TypeOf(value).String()
		}
		return zero, fmt.Errorf("di ResolveKey[%s](%q): expected type %s, actual type %s", typeName, key.Name(), typeName, actualType)
	}
	return resolved, nil
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
