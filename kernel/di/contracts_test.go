package di_test

import (
	"reflect"
	"testing"

	"github.com/cymondez/pangolin-framework/kernel/di"
)

func TestKeyIdentityIncludesServiceType(t *testing.T) {
	first, again := di.NewKey[string]("service"), di.NewKey[string]("service")
	other := di.NewKey[int]("service")
	if first.Name() != "service" || first != again || first.Type() != reflect.TypeFor[string]() || first.Type() == other.Type() {
		t.Fatal("key name/type identity lost")
	}
	var zero di.Key[string]
	if zero.Name() != "" {
		t.Fatal("zero key acquired a service name")
	}
}
