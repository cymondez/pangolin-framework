package module

import (
	"context"
	"strings"
	"testing"
	"time"
)

type regContract interface{ Tag() string }
type regImplA struct{ tag string }

func (a *regImplA) Tag() string { return a.tag }

type regImplB struct{ tag string }

func (b *regImplB) Tag() string { return b.tag }

type regConsumer struct{ c regContract }

func TestAddAndExportHelpers(t *testing.T) {
	plan := RootPlan{Plan: Plan{Ordered: []Module{
		{Name: "provider", Version: "0.1.0"},
		{Name: "consumer", Version: "0.1.0", DependsOn: []Dependency{{Name: "provider"}}},
	}}}

	for _, tc := range []struct {
		name string
		fn   func(Services) error
	}{
		{"singleton", func(s Services) error {
			return AddAndExportSingleton[regContract](s, func() *regImplA { return &regImplA{"s"} })
		}},
		{"transient", func(s Services) error {
			return AddAndExportTransient[regContract](s, func() *regImplA { return &regImplA{"t"} })
		}},
		{"operation", func(s Services) error {
			return AddAndExportOperation[regContract](s, func() *regImplA { return &regImplA{"o"} })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comp, err := NewComposition(plan, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			ps, err := comp.Services("provider")
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.fn(ps); err != nil {
				t.Fatalf("add and export %s failed: %v", tc.name, err)
			}
			cs, err := comp.Services("consumer")
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "operation" {
				if err := AddOperation(cs, func(c regContract) *regConsumer { return &regConsumer{c} }); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := AddSingleton(cs, func(c regContract) *regConsumer { return &regConsumer{c} }); err != nil {
					t.Fatal(err)
				}
			}
			container, err := comp.Seal(context.Background())
			if err != nil {
				t.Fatalf("seal failed: %v", err)
			}
			defer container.Close(context.Background())

			resolver, err := comp.LifecycleResolver("consumer")
			if err != nil {
				t.Fatal(err)
			}
			target := resolver
			if tc.name == "operation" {
				sc, err := OpenScope(context.Background(), resolver)
				if err != nil {
					t.Fatal(err)
				}
				defer sc.Close(context.Background())
				target = sc
			}
			got, err := Resolve[*regConsumer](context.Background(), target)
			if err != nil || got == nil || got.c.Tag() == "" {
				t.Fatalf("resolved consumer = %+v, %v", got, err)
			}
		})
	}

	// Nil / typed-nil services and non-interface contract fail closed.
	var typedNil *ownerServices
	if err := AddAndExportSingleton[regContract](nil, func() *regImplA { return &regImplA{} }); err == nil {
		t.Fatal("nil services accepted")
	}
	if err := AddAndExportTransient[regContract](typedNil, func() *regImplA { return &regImplA{} }); err == nil {
		t.Fatal("typed-nil services accepted")
	}
	comp, _ := NewComposition(RootPlan{Plan: Plan{Ordered: []Module{{Name: "p", Version: "0.1.0"}}}}, time.Second)
	s, _ := comp.Services("p")
	if err := AddAndExportSingleton[regImplA](s, func() *regImplA { return &regImplA{} }); err == nil || !strings.Contains(err.Error(), "interface contract") {
		t.Fatalf("non-interface contract accepted: %v", err)
	}
}

func TestFluentRegistrar(t *testing.T) {
	plan := RootPlan{Plan: Plan{Ordered: []Module{{Name: "core", Version: "0.1.0"}}}}
	comp, err := NewComposition(plan, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s, err := comp.Services("core")
	if err != nil {
		t.Fatal(err)
	}

	// Happy path chaining Singleton, Transient, Operation, Export, Bind, Replace, ReplaceContract, ReplaceBinding.
	err = Register(s).
		Singleton(func() *regImplA { return &regImplA{"a"} }, As(Contract[regContract]())).
		Transient(func() *regImplB { return &regImplB{"b"} }).
		Operation(func() regConsumer { return regConsumer{} }).
		Export(Contract[regContract](), Service[*regImplB]()).
		Bind(Contract[regContract](), (*regImplA)(nil)).
		Replace(func() *regImplB { return &regImplB{"b2"} }, "swap implB", Singleton()).
		ReplaceBinding(Contract[regContract](), (*regImplB)(nil), "bind to B").
		ReplaceContract(Contract[regContract](), func() *regImplA { return &regImplA{"a2"} }, "swap contract", Singleton()).
		Err()
	if err != nil {
		t.Fatalf("fluent registrar failed: %v", err)
	}

	// Short-circuits on first error and retains it.
	comp2, _ := NewComposition(plan, time.Second)
	s2, _ := comp2.Services("core")
	firstErr := Register(s2).
		Singleton("not-a-constructor").
		Transient(func() *regImplA { return &regImplA{} }).
		Operation(func() *regImplB { return &regImplB{} }).
		Export(Service[*regImplA]()).
		Bind(Contract[regContract](), (*regImplA)(nil)).
		Replace(func() *regImplA { return &regImplA{} }, "r").
		ReplaceContract(Contract[regContract](), func() *regImplA { return &regImplA{} }, "r").
		ReplaceBinding(Contract[regContract](), (*regImplA)(nil), "r").
		Err()
	if firstErr == nil || !strings.Contains(firstErr.Error(), "non-nil function") {
		t.Fatalf("expected first constructor error, got %v", firstErr)
	}

	// Nil / typed-nil Services and nil Registrar fail closed.
	var typedNil *ownerServices
	if err := Register(nil).Singleton(func() *regImplA { return &regImplA{} }).Err(); err == nil {
		t.Fatal("nil Services accepted")
	}
	if err := Register(typedNil).Export(Contract[regContract]()).Err(); err == nil {
		t.Fatal("typed-nil Services accepted")
	}
	var nilReg *Registrar
	if err := nilReg.Singleton(func() {}).Err(); err == nil {
		t.Fatal("nil Registrar accepted")
	}
}
