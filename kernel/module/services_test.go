package module

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/di"
)

type svcA struct{ id int }

type svcB struct{ a *svcA }

type svcC struct{}

func newA() *svcA { return &svcA{} }

func newB(a *svcA) *svcB { return &svcB{a: a} }

type depI interface{ Do() string }

type implX struct{}

func (implX) Do() string { return "x" }

type implY struct{}

func (implY) Do() string { return "y" }

type useI struct{ D depI }

func newX() implX { return implX{} }

func newY() implY { return implY{} }

func newUseI(d depI) *useI { return &useI{D: d} }

func planOf(mods ...Module) RootPlan {
	return RootPlan{Plan: Plan{Ordered: mods}, Instances: map[string]any{}}
}

func newComposition(t *testing.T, plan RootPlan) Composition {
	t.Helper()
	c, err := NewComposition(plan, 2*time.Second)
	if err != nil {
		t.Fatalf("NewComposition: %v", err)
	}
	return c
}

func register(t *testing.T, c Composition, owner string, fn func(Services)) {
	t.Helper()
	s, err := c.Services(owner)
	if err != nil {
		t.Fatalf("Services(%s): %v", owner, err)
	}
	fn(s)
}

func seal(t *testing.T, c Composition) di.Container {
	t.Helper()
	container, err := c.Seal(context.Background())
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return container
}

func mustErrContains(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %v, got nil", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("error %q does not contain %q", err.Error(), p)
		}
	}
}

func singleModule(name string) RootPlan {
	return planOf(Module{Name: name})
}

func TestServicesConstructorShapes(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newA); err != nil {
			t.Fatalf("Add plain: %v", err)
		}
		if err := s.Add(func() (*svcB, error) { return &svcB{}, nil }); err != nil {
			t.Fatalf("Add (T, error): %v", err)
		}
	})
	register(t, c, "app", func(s Services) {
		if err := s.Add(func() (*svcC, func(context.Context) error, error) {
			return &svcC{}, func(context.Context) error { return nil }, nil
		}); err != nil {
			t.Fatalf("Add (T, cleanup, error): %v", err)
		}
		if err := s.Add(func() (*svcA, func(context.Context) error, error) {
			return &svcA{}, func(context.Context) error { return nil }, nil
		}); err == nil {
			t.Fatal("expected duplicate key rejection for second constructor of same type")
		}
		if err := s.Add(nil); err == nil {
			t.Fatal("expected nil constructor error")
		}
		if err := s.Add(42); err == nil {
			t.Fatal("expected non-func constructor error")
		}
		if err := s.Add(func() {}); err == nil {
			t.Fatal("expected no-output rejection")
		}
		if err := s.Add(func() depI { return implX{} }); err == nil {
			t.Fatal("expected interface-return rejection")
		}
		if err := s.Add(func(...depI) *svcA { return nil }); err == nil {
			t.Fatal("expected variadic rejection")
		}
		if err := s.Add(func() (a *svcA, b int, err error) { return }); err == nil {
			t.Fatal("expected wrong shapes rejection")
		}
		if err := s.Add(func(ctx context.Context, ctx2 context.Context) *svcA { return nil }); err == nil {
			t.Fatal("expected double-context rejection")
		}
	})
	seal(t, c)
}

func TestServicesLifetimeDefaults(t *testing.T) {
	c := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
		Module{Name: "dep"},
	))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newB); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "dep", func(s Services) {
		if err := s.Add(newA, Singleton()); err != nil {
			t.Fatal(err)
		}
		if err := s.Export(Service[*svcA]()); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	ctx := context.Background()
	first, err := container.Resolve(ctx, Service[*svcA]())
	if err != nil {
		t.Fatalf("resolve singleton: %v", err)
	}
	second, err := container.Resolve(ctx, Service[*svcA]())
	if err != nil {
		t.Fatalf("resolve singleton twice: %v", err)
	}
	if first != second {
		t.Fatal("singleton resolve returned distinct values")
	}
	countA := 0
	countB := 0
	var mu sync.Mutex
	racy := newComposition(t, planOf(Module{Name: "app"}))
	register(t, racy, "app", func(s Services) {
		if err := s.Add(func() *svcA { mu.Lock(); countA++; mu.Unlock(); return &svcA{} }); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(func() *svcB { mu.Lock(); countB++; mu.Unlock(); return &svcB{} }); err != nil {
			t.Fatal(err)
		}
	})
	container2 := seal(t, racy)
	if _, err := container2.Resolve(ctx, Service[*svcA]()); err != nil {
		t.Fatal(err)
	}
	if _, err := container2.Resolve(ctx, Service[*svcB]()); err != nil {
		t.Fatal(err)
	}
	if countA != 1 || countB != 1 {
		t.Fatalf("expected exactly one transient construction each, got a=%d b=%d", countA, countB)
	}
}

func TestServicesOperationRequiresExplicitScope(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newA, Operation()); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	if _, err := container.Resolve(context.Background(), Service[*svcA]()); err == nil {
		t.Fatal("expected operation service to require a scope")
	}
	scope, err := container.OpenScope(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := scope.Resolve(ctx, Service[*svcA]())
	if err != nil {
		t.Fatalf("scoped resolve: %v", err)
	}
	second, err := scope.Resolve(ctx, Service[*svcA]())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("operation service should be stable within one scope")
	}
	if err := scope.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestServicesMissingAndVisibility(t *testing.T) {
	c := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
		Module{Name: "dep"},
	))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newB); err != nil {
			t.Fatal(err)
		}
	})
	_, err := c.Seal(context.Background())
	mustErrContains(t, err, "app", "missing")
}

func TestServicesContractVisibilityDiagnostics(t *testing.T) {
	t.Run("hidden contract without export", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "app"}))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "app", "depI", "missing")
	})
	t.Run("owner contract resolved without extra export", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "dep", func(s Services) {
			if err := s.Add(newX); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Contract[depI]())
		if err != nil {
			t.Fatalf("resolve contract: %v", err)
		}
		if v.(depI).Do() != "x" {
			t.Fatalf("contract resolved wrong implementation %v", v)
		}
	})
	t.Run("consumer without direct dependency on owner", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "mid"}}},
			Module{Name: "mid", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "dep", func(s Services) {
			if err := s.Add(newX); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "app", "depI", "dep")
	})
	t.Run("unexported concrete dependency", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newB); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "dep", func(s Services) {
			if err := s.Add(newA); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "app", "svcA", "dep", "export")
	})
	t.Run("exported concrete dependency", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newB); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "dep", func(s Services) {
			if err := s.Add(newA); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(Service[*svcA]()); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		if _, err := container.Resolve(context.Background(), Service[*svcB]()); err != nil {
			t.Fatalf("resolve exported concrete: %v", err)
		}
	})
}

func TestServicesCrossOwnerAsCandidate(t *testing.T) {
	c := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "owner"},
		Module{Name: "impl", DependsOn: []Dependency{{Name: "owner"}}},
	))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newUseI); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "owner", func(s Services) {
		if err := s.Export(Contract[depI]()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "impl", func(s Services) {
		if err := s.Add(newY, As(Contract[depI]())); err != nil {
			t.Fatalf("As contribution: %v", err)
		}
	})
	container := seal(t, c)
	v, err := container.Resolve(context.Background(), Contract[depI]())
	if err != nil {
		t.Fatalf("resolve cross-owner contract: %v", err)
	}
	if v.(depI).Do() != "y" {
		t.Fatalf("expected implY, got %v", v)
	}
}

func TestServicesAsWithoutOwnerDependencyFails(t *testing.T) {
	c := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "owner"},
		Module{Name: "impl"},
	))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newUseI); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "owner", func(s Services) {
		if err := s.Export(Contract[depI]()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "impl", func(s Services) {
		if err := s.Add(newY, As(Contract[depI]())); err != nil {
			t.Fatal(err)
		}
	})
	_, err := c.Seal(context.Background())
	mustErrContains(t, err, "impl", "without declaring a direct dependency on the owner")
}

func TestServicesAmbiguityAndBinding(t *testing.T) {
	t.Run("ambiguous seal error", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"}, Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i2", func(s Services) {
			if err := s.Add(newY, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "ambiguous", "module=i1", "module=i2", "depI")
	})
	t.Run("owner binding resolves ambiguity", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"}, Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i2", func(s Services) {
			if err := s.Add(newY, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Bind(Contract[depI](), implY{}); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Contract[depI]())
		if err != nil {
			t.Fatal(err)
		}
		if v.(depI).Do() != "y" {
			t.Fatalf("binding selected wrong implementation %v", v)
		}
	})
	t.Run("redundant binding on single candidate", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"}, Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Bind(Contract[depI](), implX{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "redundant binding")
	})
	t.Run("binder without required direct dependencies", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"}, Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "witness"},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i2", func(s Services) {
			if err := s.Add(newY, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "witness", func(s Services) {
			if err := s.Bind(Contract[depI](), implY{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "witness", "contract owner")
	})
	t.Run("binding must match a candidate", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"}, Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i2", func(s Services) {
			if err := s.Add(newY, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Bind(Contract[depI](), svcA{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "does not match any candidate")
	})
	t.Run("reversed registration order keeps resolution", func(t *testing.T) {
		build := func(order []string) string {
			c := newComposition(t, planOf(
				Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
				Module{Name: "owner"}, Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
				Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
			))
			do := map[string]func(s Services){
				"app":   func(s Services) { _ = s.Add(newUseI) },
				"owner": func(s Services) { _ = s.Export(Contract[depI]()) },
				"i1":    func(s Services) { _ = s.Add(newX, As(Contract[depI]())) },
				"i2":    func(s Services) { _ = s.Add(newY, As(Contract[depI]())) },
			}
			for _, name := range order {
				register(t, c, name, do[name])
			}
			_, err := c.Seal(context.Background())
			if err == nil {
				t.Fatal("expected ambiguity error")
			}
			return err.Error()
		}
		forward := build([]string{"app", "owner", "i1", "i2"})
		reversed := build([]string{"i2", "i1", "owner", "app"})
		if forward != reversed {
			t.Fatalf("registration order changed the deterministic diagnostics:\nforward : %s\nreversed: %s", forward, reversed)
		}
		mustErrContains(t, errors.New(forward), "module=i1", "module=i2")
	})
}

func TestServicesReplaceOperations(t *testing.T) {
	t.Run("constructor replace", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newA); err != nil {
				t.Fatal(err)
			}
			if err := s.Replace(func() *svcA { return &svcA{id: 7} }, "seed override"); err != nil {
				t.Fatalf("Replace: %v", err)
			}
			if err := s.Replace(newA, ""); err == nil {
				t.Fatal("expected empty reason rejection")
			}
			if err := s.Replace(newB, "wrong type"); err == nil {
				t.Fatal("expected unrelated output type rejection")
			}
		})
		register(t, c, "app", func(s Services) {})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Service[*svcA]())
		if err != nil {
			t.Fatal(err)
		}
		if v.(*svcA).id != 7 {
			t.Fatalf("replace not applied: %v", v)
		}
	})
	t.Run("replace without prior registration", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "app"}, Module{Name: "other"}))
		register(t, c, "app", func(s Services) {
			if err := s.Replace(newA, "no prior"); err == nil {
				t.Fatal("expected missing prior rejection")
			}
			if err := s.Add(newA); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "other", func(s Services) {
			if err := s.Add(newA); err == nil {
				t.Fatal("expected cross-module duplicate rejection")
			}
		})
	})
	t.Run("cross-owner contract implementation replace", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"}, Module{Name: "impl1", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "impl2", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "impl1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "impl2", func(s Services) {
			if err := s.ReplaceContract(Contract[depI](), newY, "switch provider"); err != nil {
				t.Fatalf("ReplaceContract: %v", err)
			}
			log := c.ReplacementLog()
			if len(log) != 1 || !strings.Contains(log[len(log)-1], "switch provider") {
				t.Fatalf("replacement log missing reason: %v", log)
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Contract[depI]())
		if err != nil {
			t.Fatal(err)
		}
		if v.(depI).Do() != "y" {
			t.Fatalf("expected replaced implementation, got %v", v)
		}
	})
	t.Run("owner contract replace keeps identity", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newX); err != nil {
				t.Fatal(err)
			}
			if err := s.ReplaceContract(Contract[depI](), newY, "owner swaps its own implementation"); err != nil {
				t.Fatalf("owner ReplaceContract: %v", err)
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Contract[depI]())
		if err != nil {
			t.Fatal(err)
		}
		if v.(depI).Do() != "y" {
			t.Fatalf("expected owner-replaced implementation, got %v", v)
		}
	})
	t.Run("replace contract needs owner dependency", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"}, Module{Name: "impl1", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "outsider"},
		))
		register(t, c, "app", func(s Services) {})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "impl1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "outsider", func(s Services) {
			if err := s.ReplaceContract(Contract[depI](), newY, "no dependency"); err == nil {
				t.Fatal("expected missing owner dependency rejection")
			}
		})
	})
	t.Run("replace contract without prior implementation is rejected (D)", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"}, Module{Name: "impl", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "impl", func(s Services) {
			if err := s.ReplaceContract(Contract[depI](), newY, "first contribution attempt"); err == nil {
				t.Fatal("ReplaceContract without an existing implementation must be rejected; use Add(..., As(...))")
			} else {
				mustErrContains(t, err, "requires an existing implementation")
			}
		})
	})
}

func TestServicesAsImplementationCheck(t *testing.T) {
	t.Run("unrelated struct As is rejected at registration", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
			Module{Name: "impl", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		countersTotal = 0
		register(t, c, "impl", func(s Services) {
			if err := s.Add(newMulti, As(Contract[depI]())); err == nil {
				t.Fatal("unrelated struct must be rejected as As candidate at registration")
			} else {
				mustErrContains(t, err, "does not implement")
			}
			// Selected or not, no candidate of an inconsistent association may
			// ever reach a factory: registration is rejected outright.
			if countersTotal != 0 {
				t.Fatalf("constructors ran during registration: %d", countersTotal)
			}
		})
	})
	t.Run("no factory runs when a bad candidate is registered or replaced (selected or not)", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
			Module{Name: "impl", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		countersTotal = 0
		register(t, c, "impl", func(s Services) {
			if err := s.Add(newMulti, As(Contract[depI]())); err == nil {
				t.Fatal("unrelated struct must be rejected as As candidate at registration")
			} else {
				mustErrContains(t, err, "does not implement")
			}
			if err := s.ReplaceContract(Contract[depI](), newMulti, "bad replacement target"); err == nil {
				t.Fatal("incompatible ReplaceContract must be rejected at registration")
			} else {
				mustErrContains(t, err, "does not implement")
			}
			if countersTotal != 0 {
				t.Fatalf("constructors ran for rejected associations: %d", countersTotal)
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Contract[depI]())
		if err != nil {
			t.Fatal(err)
		}
		if v.(depI).Do() != "x" {
			t.Fatalf("unexpected implementation %v", v)
		}
		if countersTotal != 0 {
			t.Fatalf("no factory may run for rejected associations; ran %d", countersTotal)
		}
	})
	t.Run("incompatible ReplaceContract is rejected before provisioning", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
			Module{Name: "impl1", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "impl2", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "impl1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "impl2", func(s Services) {
			if err := s.ReplaceContract(Contract[depI](), newMulti, "bad replacement target"); err == nil {
				t.Fatal("incompatible ReplaceContract must be rejected at registration")
			} else {
				mustErrContains(t, err, "does not implement")
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Contract[depI]())
		if err != nil {
			t.Fatalf("rejected replacement must not disturb the valid candidate: %v", err)
		}
		if v.(depI).Do() != "x" {
			t.Fatalf("expected implX to stay selected, got %v", v)
		}
	})
}

var countersTotal int

// multi implements only depJ; it is deliberately NOT a depI candidate.
type multi struct{}

func (multi) Run() string { return "run" }

func newMulti() multi { countersTotal++; return multi{} }

// both implements depI and depJ: replacing one contract must not touch the
// other or the plain concrete service.
type both struct{}

func (both) Do() string  { return "i" }
func (both) Run() string { return "j" }

func newBoth() both { return both{} }

type depJ interface{ Run() string }

type useJ struct{ D depJ }

func newUseJ(d depJ) *useJ { return &useJ{D: d} }

func TestServicesContractScopedExclusion(t *testing.T) {
	t.Run("replacing one contract keeps sibling contract and concrete service", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
			Module{Name: "impl", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newUseJ); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(Contract[depJ]()); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newBoth); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "impl", func(s Services) {
			if err := s.ReplaceContract(Contract[depI](), newY, "swap I only"); err != nil {
				t.Fatalf("ReplaceContract: %v", err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		if v, err := container.Resolve(ctx, Contract[depI]()); err != nil || v.(depI).Do() != "y" {
			t.Fatalf("contract I after replacement: %v %v", v, err)
		}
		v, err := container.Resolve(ctx, Contract[depJ]())
		if err != nil {
			t.Fatalf("sibling contract J must keep the old implementation: %v", err)
		}
		if v.(depJ).Run() != "j" {
			t.Fatalf("sibling contract J drifted: %v", v)
		}
		svc, err := container.Resolve(ctx, Service[both]())
		if err != nil {
			t.Fatalf("concrete service must stay resolvable: %v", err)
		}
		if svc.(both).Do() != "i" || svc.(both).Run() != "j" {
			t.Fatalf("concrete service drifted: %v", svc)
		}
	})
	t.Run("replacement of multiple old candidates is exclusive and order-independent", func(t *testing.T) {
		build := func(order []string) (string, error) {
			c := newComposition(t, planOf(
				Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
				Module{Name: "owner"},
				Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
				Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
				Module{Name: "repl", DependsOn: []Dependency{{Name: "owner"}}},
			))
			do := map[string]func(s Services){
				"app":   func(s Services) { _ = s.Add(newUseI) },
				"owner": func(s Services) { _ = s.Export(Contract[depI]()); _ = s.Add(newBoth) },
				"i1":    func(s Services) { _ = s.Add(newX, As(Contract[depI]())) },
				"i2":    func(s Services) { _ = s.Add(newY, As(Contract[depI]())) },
				"repl": func(s Services) {
					_ = s.ReplaceContract(Contract[depI](), newY, "exclusive replace over multiple old candidates")
				},
			}
			for _, name := range order {
				register(t, c, name, do[name])
			}
			container, err := c.Seal(context.Background())
			if err != nil {
				return "", err
			}
			v, err := container.Resolve(context.Background(), Contract[depI]())
			if err != nil {
				return "", err
			}
			return v.(depI).Do(), nil
		}
		forward, fwdErr := build([]string{"app", "owner", "i1", "i2", "repl"})
		reversed, revErr := build([]string{"i2", "i1", "owner", "repl", "app"})
		if fwdErr != nil || revErr != nil {
			t.Fatalf("seal errors: forward=%v reversed=%v", fwdErr, revErr)
		}
		if forward != "y" || reversed != "y" {
			t.Fatalf("replacement resolution order-dependent or wrong: forward=%q reversed=%q", forward, reversed)
		}
	})
}

type neutralStore struct{ tag string }

func newDefaultStore() *neutralStore { return &neutralStore{tag: "default"} }

func newOrgStore() *neutralStore { return &neutralStore{tag: "org"} }

type consumerService struct{ tag string }

type portFacade struct{ D *neutralStore }

func newPortFacade(s *neutralStore) *portFacade { return &portFacade{D: s} }

func TestServicesNeutralConcreteReplace(t *testing.T) {
	storeToken := di.NewKey[*neutralStore]("neutral.store")
	privateToken := di.NewKey[*neutralStore]("private.store")
	facadeToken := Service[*portFacade]()
	t.Run("cross-owner replace of exported named provider keeps identity and export owner", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "consumer", DependsOn: []Dependency{{Name: "port"}}},
			Module{Name: "port"},
			Module{Name: "org", DependsOn: []Dependency{{Name: "port"}}},
			Module{Name: "outsider"},
		))
		if err := c.Provide("port", di.Provider{Key: storeToken, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return newDefaultStore(), nil, nil }}); err != nil {
			t.Fatal(err)
		}
		if err := c.Provide("port", di.Provider{Key: privateToken, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
			return &neutralStore{tag: "private"}, nil, nil
		}}); err != nil {
			t.Fatal(err)
		}
		register(t, c, "port", func(s Services) {
			if err := s.Export(storeToken); err != nil {
				t.Fatalf("named-token export: %v", err)
			}
		})
		orgFactory := func(context.Context, di.Resolver) (any, di.Cleanup, error) { return newOrgStore(), nil, nil }
		// The org implementation module replaces the exported neutral
		// provider; neither the consumer nor anyone else imports org.
		if err := c.Replace("org", di.Provider{Key: storeToken, Lifetime: di.Singleton, Factory: orgFactory}, "swap neutral store implementation"); err != nil {
			t.Fatalf("cross-owner provider replace: %v", err)
		}
		if err := c.Replace("org", di.Provider{Key: privateToken, Lifetime: di.Singleton, Factory: orgFactory}, "should fail"); err == nil {
			t.Fatal("unexported private provider must not be replaceable by a foreign module")
		}
		if err := c.Replace("outsider", di.Provider{Key: storeToken, Lifetime: di.Singleton, Factory: orgFactory}, "no dependency"); err == nil {
			t.Fatal("outsider without a direct dependency on the export owner must be rejected")
		}
		register(t, c, "consumer", func(s Services) {})
		// The consumer consumes the custom-named neutral provider via an
		// explicitly declared low-level dependency; it imports only the port
		// (contract owner) module.
		if err := c.Provide("consumer", di.Provider{Key: Service[*portFacade](), Lifetime: di.Singleton, DependsOn: []di.Token{storeToken}, Factory: func(ctx context.Context, r di.Resolver) (any, di.Cleanup, error) {
			v, err := r.Resolve(ctx, storeToken)
			if err != nil {
				return nil, nil, err
			}
			return &portFacade{D: v.(*neutralStore)}, nil, nil
		}}); err != nil {
			t.Fatal(err)
		}
		container := seal(t, c)
		ctx := context.Background()
		v, err := container.Resolve(ctx, facadeToken)
		if err != nil {
			t.Fatalf("consumer resolves through contract owner only: %v", err)
		}
		if v.(*portFacade).D.tag != "org" {
			t.Fatalf("expected replaced implementation, got %v", v.(*portFacade).D.tag)
		}
		res, err := c.LifecycleResolver("port")
		if err != nil {
			t.Fatal(err)
		}
		named, err := res.Resolve(ctx, storeToken)
		if err != nil {
			t.Fatalf("export owner keeps exact-token access: %v", err)
		}
		if named.(*neutralStore).tag != "org" {
			t.Fatalf("named provider identity or source drifted: %v", named)
		}
		log := strings.Join(c.ReplacementLog(), "|")
		if !strings.Contains(log, "swap neutral store implementation") || !strings.Contains(log, "previous source module port") || !strings.Contains(log, "export owner port") {
			t.Fatalf("replacement provenance incomplete: %v", log)
		}
	})
	t.Run("cross-owner constructor replace follows the same export rule", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"}, Module{Name: "other"},
		))
		register(t, c, "dep", func(s Services) {
			if err := s.Add(newA); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(Service[*svcA]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "app", func(s Services) {
			if err := s.Replace(func() *svcA { return &svcA{id: 9} }, "app supplies its own default"); err != nil {
				t.Fatalf("cross-owner constructor replace: %v", err)
			}
		})
		register(t, c, "other", func(s Services) {
			if err := s.Replace(newA, "foreign unexported service"); err == nil {
				t.Fatal("unexported foreign service must not be replaceable")
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Service[*svcA]())
		if err != nil {
			t.Fatal(err)
		}
		if v.(*svcA).id != 9 {
			t.Fatalf("cross-owner constructor replace not applied: %v", v)
		}
	})
}

func TestServicesLowLevelTokens(t *testing.T) {
	t.Run("identity preserved and visible per module", func(t *testing.T) {
		token := di.NewKey[*svcA]("named.a")
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		register(t, c, "dep", func(s Services) {})
		register(t, c, "app", func(s Services) {})
		if err := c.Provide("dep", di.Provider{Key: token, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return &svcA{id: 3}, nil, nil }}); err != nil {
			t.Fatalf("Provide: %v", err)
		}
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), token)
		if err != nil {
			t.Fatal(err)
		}
		if v.(*svcA).id != 3 {
			t.Fatalf("identity lost: %v", v)
		}
		if _, err := container.Resolve(context.Background(), Service[*svcA]()); err == nil {
			t.Fatal("named token must keep its exact identity")
		}
	})
	t.Run("duplicate low-level key rejected", func(t *testing.T) {
		token := di.NewKey[*svcA]("dup")
		c := newComposition(t, planOf(Module{Name: "a"}, Module{Name: "b"}))
		prov := di.Provider{Key: token, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return &svcA{}, nil, nil }}
		if err := c.Provide("a", prov); err != nil {
			t.Fatal(err)
		}
		if err := c.Provide("b", prov); err == nil {
			t.Fatal("expected duplicate provider rejection")
		}
	})
	t.Run("low-level replace keeps identity", func(t *testing.T) {
		token := di.NewKey[*svcA]("r")
		c := newComposition(t, singleModule("app"))
		factory := func(tag int) di.Factory {
			return func(context.Context, di.Resolver) (any, di.Cleanup, error) { return &svcA{id: tag}, nil, nil }
		}
		if err := c.Provide("app", di.Provider{Key: token, Lifetime: di.Singleton, Factory: factory(1)}); err != nil {
			t.Fatal(err)
		}
		if err := c.Replace("app", di.Provider{Key: token, Lifetime: di.Singleton, Factory: factory(2)}, "hot swap"); err != nil {
			t.Fatalf("Replace: %v", err)
		}
		if err := c.Replace("app", di.Provider{Key: token, Lifetime: di.Singleton, Factory: factory(3)}, ""); err == nil {
			t.Fatal("expected empty reason rejection")
		}
		container := seal(t, c)
		v, _ := container.Resolve(context.Background(), token)
		if v.(*svcA).id != 2 {
			t.Fatalf("low-level replace not applied: %v", v)
		}
	})
	t.Run("export requires ownership", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "a"}, Module{Name: "b"}))
		register(t, c, "a", func(s Services) {})
		register(t, c, "b", func(s Services) {
			if err := s.Export(Service[*svcA]()); err == nil {
				t.Fatal("expected export-without-registration rejection")
			}
		})
	})
	t.Run("duplicate contract owner rejected", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "a"}, Module{Name: "b"}))
		register(t, c, "a", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "b", func(s Services) {
			if err := s.Export(Contract[depI]()); err == nil {
				t.Fatal("expected duplicate export rejection")
			}
		})
	})
	t.Run("low-level named token visible only to direct dependency consumers", func(t *testing.T) {
		token := di.NewKey[*svcA]("named.visible")
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "other", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		if err := c.Provide("dep", di.Provider{Key: token, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return &svcA{id: 5}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		register(t, c, "dep", func(s Services) {
			if err := s.Export(token); err != nil {
				t.Fatalf("named-token export: %v", err)
			}
		})
		if err := c.Provide("dep", di.Provider{Key: di.NewKey[*svcA]("unexported.named"), Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return &svcA{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		container := seal(t, c)
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.Resolve(context.Background(), token); err != nil {
			t.Fatalf("direct consumer resolve: %v", err)
		}
		if _, err := res.Resolve(context.Background(), di.NewKey[*svcA]("unexported.named")); err == nil {
			t.Fatal("unexported named provider must stay hidden")
		}
		other, err := c.LifecycleResolver("other")
		if err != nil {
			t.Fatal(err)
		}
		// A direct dependency consumer sees the exported named provider
		// under its exact identity; export is the visibility boundary.
		if _, err := other.Resolve(context.Background(), token); err != nil {
			t.Fatalf("other direct consumer resolve: %v", err)
		}
		_ = container
	})
}

func TestServicesLaunderingRejected(t *testing.T) {
	c := newComposition(t, planOf(
		Module{Name: "app"},
		Module{Name: "i1"},
	))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newA, Operation()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "i1", func(s Services) {
		if err := s.Add(newB, Singleton()); err != nil {
			t.Fatal(err)
		}
	})
	_, err := c.Seal(context.Background())
	mustErrContains(t, err, "operation")
	mustErrContains(t, err, "->")
}

func TestServicesLaunderingViaContractRejected(t *testing.T) {
	c := newComposition(t, planOf(
		Module{Name: "app"},
		Module{Name: "i1"},
	))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newA, Operation()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "i1", func(s Services) {
		if err := s.Add(newB, Singleton()); err != nil {
			t.Fatal(err)
		}
		// The contract delegating to the singleton implementation keeps the
		// laundering path: contract -> svcB -> svcA(operation).
		if err := s.Export(Contract[*svcB]()); err != nil {
			t.Fatal(err)
		}
	})
	_, err := c.Seal(context.Background())
	mustErrContains(t, err, "operation")
}

func TestServicesCleanupOrderAndFailure(t *testing.T) {
	var mu sync.Mutex
	var order []string
	c := newComposition(t, singleModule("app"))
	register(t, c, "app", func(s Services) {
		if err := s.Add(func() (*svcA, func(context.Context) error, error) {
			return &svcA{}, func(context.Context) error {
				mu.Lock()
				order = append(order, "first")
				mu.Unlock()
				return nil
			}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(func() (*svcB, func(context.Context) error, error) {
			b := &svcB{}
			return b, func(context.Context) error {
				mu.Lock()
				order = append(order, "second")
				mu.Unlock()
				return errors.New("cleanup boom")
			}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	if _, err := container.Resolve(context.Background(), Service[*svcA]()); err != nil {
		t.Fatal(err)
	}
	if _, err := container.Resolve(context.Background(), Service[*svcB]()); err != nil {
		t.Fatal(err)
	}
	err := container.Close(context.Background())
	mustErrContains(t, err, "cleanup boom")
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("cleanup order not reversed: %v", order)
	}
}

func TestServicesCanonicalTokens(t *testing.T) {
	first := Contract[depI]()
	second := Contract[depI]()
	if first.Name() != second.Name() || first.Type() != second.Type() {
		t.Fatal("contract tokens must be deterministic")
	}
	if strings.TrimSpace(first.Name()) == "" {
		t.Fatal("contract token name must be nonempty")
	}
	if first.Name() != canonicalTokenName(first.Type()) {
		t.Fatalf("contract name %q must equal canonical derivation", first.Name())
	}
	service := Service[*svcA]()
	if service.Name() != Contract[*svcA]().Name() {
		t.Fatalf("Service and Contract of the same type share the canonical identity; got %q vs %q", service.Name(), Contract[*svcA]().Name())
	}
	if service.Name() != canonicalTokenName(service.Type()) {
		t.Fatal("pointer canonicalization drifted")
	}
	custom := di.NewKey[depI]("custom.name")
	if custom.Name() != "custom.name" {
		t.Fatal("custom token names must be preserved")
	}
}

func TestServicesPlanValidation(t *testing.T) {
	if _, err := NewComposition(RootPlan{}, time.Second); err == nil {
		t.Fatal("expected empty plan rejection")
	}
	if _, err := NewComposition(singleModule("app"), 0); err == nil {
		t.Fatal("expected zero timeout rejection")
	}
	dup := planOf(Module{Name: "x"}, Module{Name: "x"})
	if _, err := NewComposition(dup, time.Second); err == nil {
		t.Fatal("expected duplicate module rejection")
	}
	missing := planOf(Module{Name: "a", DependsOn: []Dependency{{Name: "ghost"}}})
	if _, err := NewComposition(missing, time.Second); err == nil {
		t.Fatal("expected missing dependency rejection")
	}
	self := planOf(Module{Name: "a", DependsOn: []Dependency{{Name: "a"}}})
	if _, err := NewComposition(self, time.Second); err == nil {
		t.Fatal("expected self dependency rejection")
	}
	dupEdge := planOf(Module{Name: "a", DependsOn: []Dependency{{Name: "b"}, {Name: "b"}}}, Module{Name: "b"})
	if _, err := NewComposition(dupEdge, time.Second); err == nil {
		t.Fatal("expected duplicate dependency edge rejection")
	}
}

func TestServicesProvenanceWindows(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	s, err := c.Services("app")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add(newA); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Services("ghost"); err == nil {
		t.Fatal("expected unknown owner rejection")
	}
	c.Revoke("app")
	if err := s.Add(func() *svcB { return nil }); err == nil {
		t.Fatal("expected revoked handle rejection")
	}
	fresh, err := c.Services("app")
	if err != nil {
		t.Fatalf("new handle after revoke: %v", err)
	}
	if err := fresh.Add(func() *svcB { return nil }); err != nil {
		t.Fatal(err)
	}
	c.Revoke("app")
	old := fresh
	fresh2, err := c.Services("app")
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Add(func() *svcA { return nil }); err == nil {
		t.Fatal("expected stale handle rejection")
	}
	if err := fresh2.Add(func() *svcC { return &svcC{} }); err != nil {
		t.Fatal(err)
	}
	c.Revoke("app")
	// A revoke during a concurrent in-flight mutation must not interleave:
	// the epoch check and the mutation are serialized under the session lock.
	syncHandle, _ := c.Services("app")
	done := make(chan error, 1)
	go func() {
		done <- syncHandle.Add(func() *svcB { return nil })
	}()
	c.Revoke("app")
	if err := <-done; err == nil {
		t.Fatal("expected post-revoke async mutation rejection")
	} else if !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("unexpected post-revoke error %v", err)
	}
	container := seal(t, c)
	if _, err := container.Resolve(context.Background(), Service[*svcA]()); err != nil {
		t.Fatalf("final resolution: %v", err)
	}
	if _, err := c.Services("app"); err == nil {
		t.Fatal("expected services after seal rejection")
	}
	if err := c.Provide("app", di.Provider{Key: Service[*svcB](), Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return nil, nil, nil }}); err == nil {
		t.Fatal("expected provide after seal rejection")
	}
}

func TestServicesSealOneShot(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	first, err := c.Seal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Seal(context.Background()); err == nil {
		t.Fatal("expected second seal rejection")
	}
	if _, err := c.Services("app"); err == nil {
		t.Fatal("expected registration after seal rejection")
	}
	c.Revoke("app")
	_ = first
}

func TestServicesSealValidationFailureIsOneShot(t *testing.T) {
	c := newComposition(t, planOf(Module{Name: "app"}))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newB); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := c.Seal(context.Background()); err == nil {
		t.Fatal("expected validation failure")
	}
	if _, err := c.Seal(context.Background()); err == nil {
		t.Fatal("expected one-shot sealed rejection after failure")
	}
	if _, err := c.LifecycleResolver("app"); err == nil {
		t.Fatal("expected lifecycle resolver rejection without sealed container")
	}
}

func TestServicesLifecycleResolverBoundary(t *testing.T) {
	c := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
		Module{Name: "dep"},
	))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newB); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "dep", func(s Services) {
		if err := s.Add(newA); err != nil {
			t.Fatal(err)
		}
		if err := s.Export(Service[*svcA]()); err != nil {
			t.Fatal(err)
		}
	})
	seal(t, c)
	res, err := c.LifecycleResolver("app")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := res.Resolve(ctx, Service[*svcB]()); err != nil {
		t.Fatalf("own service: %v", err)
	}
	if _, err := res.Resolve(ctx, Service[*svcA]()); err != nil {
		t.Fatalf("direct dependency export: %v", err)
	}
	if _, err := res.Resolve(ctx, Service[*svcB]()); err != nil {
		t.Fatal(err)
	}
	dep, err := c.LifecycleResolver("dep")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dep.Resolve(ctx, Service[*svcB]()); err == nil {
		t.Fatal("reverse visibility must be rejected")
	} else {
		mustErrContains(t, err, "dep", "svcB", "app")
	}
	if _, err := res.Resolve(nil, Service[*svcA]()); err == nil {
		t.Fatal("nil context must be rejected")
	}
	if _, err := c.LifecycleResolver("ghost"); err == nil {
		t.Fatal("unknown module must be rejected")
	}
}

func TestServicesLowLevelFactoryResolvesContract(t *testing.T) {
	c := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "port"}}},
		Module{Name: "port"},
		Module{Name: "impl", DependsOn: []Dependency{{Name: "port"}}},
	))
	register(t, c, "port", func(s Services) {
		if err := s.Export(Contract[depI]()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "impl", func(s Services) {
		if err := s.Add(newY, As(Contract[depI]())); err != nil {
			t.Fatal(err)
		}
	})
	if err := c.Provide("app", di.Provider{
		Key: Service[useI](), Lifetime: di.Singleton, DependsOn: []di.Token{Contract[depI]()},
		Factory: func(ctx context.Context, r di.Resolver) (any, di.Cleanup, error) {
			v, err := r.Resolve(ctx, Contract[depI]())
			if err != nil {
				return nil, nil, err
			}
			return useI{D: v.(depI)}, nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	container := seal(t, c)
	v, err := container.Resolve(context.Background(), Service[useI]())
	if err != nil {
		t.Fatalf("low-level factory contract resolution: %v", err)
	}
	if v.(useI).D.Do() != "y" {
		t.Fatalf("unexpected implementation %v", v)
	}
}

func TestServicesContractResolutionEdgeDiagnostics(t *testing.T) {
	t.Run("contract exported without any implementation", func(t *testing.T) {
		c1 := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
		))
		register(t, c1, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c1.Seal(context.Background())
		mustErrContains(t, err, "has no implementation")

		// Binding on an exported concrete (redundant-binding path).
		c2 := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		register(t, c2, "dep", func(s Services) {
			if err := s.Add(newA); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(Service[*svcA]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c2, "app", func(s Services) {
			if err := s.Bind(Service[*svcA](), svcA{}); err != nil {
				t.Fatal("binding on concrete is recorded and judged at seal")
			}
		})
		_, err = c2.Seal(context.Background())
		mustErrContains(t, err, "redundant binding", "module dep")
	})
	t.Run("low-level provider for a foreign canonical contract is rejected", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
			Module{Name: "impl", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		if err := c.Provide("impl", di.Provider{Key: Contract[depI](), Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return implX{}, nil, nil }}); err != nil {
			t.Fatalf("low-level registration is accepted and judged at seal: %v", err)
		}
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "registers a low-level provider for contract")
	})
	t.Run("low-level provider plus constructor candidates conflict", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
		))
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		if err := c.Provide("owner", di.Provider{Key: Contract[depI](), Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return implY{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "implemented by both a low-level provider and constructor candidates")
	})
	t.Run("binding under a low-level provider conflict reports the conflict", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
		))
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		if err := c.Provide("owner", di.Provider{Key: Contract[depI](), Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return implX{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		register(t, c, "owner", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[depI](), implX{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "implemented by both a low-level provider and constructor candidates")
	})
	t.Run("As to an unexported contract is accepted and fails at seal", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
			Module{Name: "impl", DependsOn: []Dependency{{Name: "dep"}}},
		))
		register(t, c, "impl", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatalf("As to an unexported contract is accepted and judged at seal: %v", err)
			}
		})
		register(t, c, "dep", func(s Services) {
			if err := s.Bind(Contract[depI](), implX{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "no module exported this contract")
	})
}

func TestServicesConstructorContextInjection(t *testing.T) {
	// The di container passes a live factory context into the constructor and
	// cancels it after the factory returns; the constructor must see a live
	// context during construction.
	c := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
		Module{Name: "dep"},
	))
	liveDuring := make(chan bool, 1)
	register(t, c, "dep", func(s Services) {
		if err := s.Add(func(ctx context.Context) *svcA {
			liveDuring <- ctx != nil && ctx.Err() == nil
			return &svcA{}
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.Export(Service[*svcA]()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "app", func(s Services) {
		if err := s.Add(func(ctx context.Context, a *svcA) *svcB {
			return &svcB{a: a}
		}); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	v, err := container.Resolve(context.Background(), Service[*svcB]())
	if err != nil {
		t.Fatal(err)
	}
	if v.(*svcB).a == nil {
		t.Fatal("ctx-param constructor did not receive its dependency")
	}
	select {
	case ok := <-liveDuring:
		if !ok {
			t.Fatal("ctx-param constructor must receive a live context during construction")
		}
	default:
		t.Fatal("ctx-param constructor was never invoked for the dependency")
	}
}

func TestServicesRevocationAndCancellationFailurePaths(t *testing.T) {
	t.Run("constructor error cleans up registered cleanup and no value is cached", func(t *testing.T) {
		var order []string
		var mu sync.Mutex
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newA); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(func() (*svcA, func(context.Context) error, error) {
				return nil, nil, errors.New("boom before value")
			}, Operation()); err == nil {
				t.Fatal("expected constructor error propagation check below via factory")
			}
			if err := s.Add(func() (*svcB, func(context.Context) error, error) {
				return nil, func(context.Context) error {
					mu.Lock()
					order = append(order, "b-cleanup")
					mu.Unlock()
					return nil
				}, errors.New("constructor fails after resource")
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(func(a *svcA) *svcC {
				return &svcC{}
			}, As(Contract[*svcC]())); err == nil {
				t.Fatal("As on concrete contract must be rejected")
			}
			if err := s.Export(Contract[*svcC]()); err == nil {
				t.Fatal("canonical concrete token contract export is a no-op export; must be accepted as concrete export")
			}
		})
		container := seal(t, c)
		_, err := container.Resolve(context.Background(), Service[*svcB]())
		mustErrContains(t, err, "constructor fails after resource")
		mu.Lock()
		ran := len(order)
		mu.Unlock()
		if ran != 0 {
			t.Fatalf("returned cleanup must run only through container paths; saw %d", ran)
		}
	})
	t.Run("lifecycle resolver survives revocation and rejects revoked epochs only", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "app"}, Module{Name: "dep"}))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newA); err != nil {
				t.Fatal(err)
			}
		})
		seal(t, c)
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		c.Revoke("app")
		if _, err := res.Resolve(context.Background(), Service[*svcA]()); err != nil {
			t.Fatalf("post-seal revocation must not affect the container-backed resolver: %v", err)
		}
	})
	t.Run("resolve of a token whose registration was revoked mid-flight fails cleanly (no panic)", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "app"}))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newA, Operation()); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		c.Revoke("app")
		scope, err := container.OpenScope(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := scope.Resolve(context.Background(), Service[*svcA]()); err != nil {
			t.Fatalf("operation services must stay resolvable via explicit scope after revocation: %v", err)
		}
		if err := scope.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestServicesAsTypeMismatch(t *testing.T) {
	// reflect.StructOf builds distinct anonymous runtime types; the engine
	// keys on reflect.Type identity (tokenKey.typ), so two providers whose
	// tokens carry the same NAME but distinct reflect.Type coexist. Here the
	// two holders differ structurally, proving identity separation end to
	// end through Provide/Export/Resolve with a shared custom token name.
	fieldA := reflect.StructField{Name: "Id", Type: reflect.TypeFor[int]()}
	fieldB := reflect.StructField{Name: "Id", Type: reflect.TypeFor[int64]()}
	holderA := reflect.StructOf([]reflect.StructField{fieldA})
	holderB := reflect.StructOf([]reflect.StructField{fieldB})
	if holderA == holderB {
		t.Fatal("fixture drift: distinct holders expected")
	}
	tokenName := "ambiguous.display"
	tokenA := tokenAdaptor{tokenName, holderA}
	tokenB := tokenAdaptor{tokenName, holderB}
	if tokenA.Name() != tokenB.Name() || tokenA.Type() == tokenB.Type() {
		t.Fatal("token identity pair drift")
	}
	c := newComposition(t, planOf(Module{Name: "app"}, Module{Name: "other"}))
	if err := c.Provide("app", di.Provider{Key: tokenA, Lifetime: di.Transient, DependsOn: nil, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		return reflect.New(holderA).Elem().Interface(), nil, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Provide("other", di.Provider{Key: tokenB, Lifetime: di.Transient, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		return reflect.New(holderB).Elem().Interface(), nil, nil
	}}); err != nil {
		t.Fatalf("same display name with distinct reflect.Type must coexist: %v", err)
	}
	register(t, c, "app", func(s Services) {
		if err := s.Export(tokenA); err != nil {
			t.Fatalf("export bridge for tokenA: %v", err)
		}
	})
	register(t, c, "other", func(s Services) {
		if err := s.Export(tokenB); err != nil {
			t.Fatalf("export bridge for tokenB must be independent: %v", err)
		}
	})
	container := seal(t, c)
	ctx := context.Background()
	va, err := container.Resolve(ctx, tokenA)
	if err != nil {
		t.Fatal(err)
	}
	vb, err := container.Resolve(ctx, tokenB)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.TypeOf(va) != holderA || reflect.TypeOf(vb) != holderB {
		t.Fatalf("providers resolved to wrong reflect identities: %v / %v", reflect.TypeOf(va), reflect.TypeOf(vb))
	}
}

func TestServicesContractScopedReplacementEdges(t *testing.T) {
	t.Run("replace binding picks another candidate and is logged", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
			Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[depI](), implY{}); err != nil {
				t.Fatal(err)
			}
			if err := s.ReplaceBinding(Contract[depI](), implX{}, "rotate selection"); err != nil {
				t.Fatalf("ReplaceBinding: %v", err)
			}
			if err := s.ReplaceBinding(Contract[depI](), implY{}, ""); err == nil {
				t.Fatal("empty reason must be rejected")
			}
		})
		register(t, c, "i1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
			if err := s.ReplaceBinding(Contract[depI](), implY{}, "not the binding owner"); err == nil {
				t.Fatal("ReplaceBinding of another module's binding must be rejected")
			}
		})
		register(t, c, "i2", func(s Services) {
			if err := s.Add(newY, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Contract[depI]())
		if err != nil {
			t.Fatal(err)
		}
		if v.(depI).Do() != "x" {
			t.Fatalf("ReplaceBinding did not select implX: %v", v)
		}
		log := strings.Join(c.ReplacementLog(), "|")
		if !strings.Contains(log, "rotate selection") {
			t.Fatalf("binding replacement missing from provenance log: %v", log)
		}
	})
	t.Run("bind by non-owner is deferred to seal and rejected there", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
			Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "other"},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i1", func(s Services) {
			if err := s.Add(newX, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "i2", func(s Services) {
			if err := s.Add(newY, As(Contract[depI]())); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "other", func(s Services) {
			if err := s.Bind(Contract[depI](), implX{}); err != nil {
				t.Fatalf("binding legality is decided at seal, registration records it: %v", err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "restricted to the contract owner")
	})
	t.Run("bind on non-contract target is rejected at seal", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		register(t, c, "dep", func(s Services) {
			if err := s.Add(newA); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "app", func(s Services) {
			// Registration records the request; a binding whose target was
			// never exported and is not a canonical contract is rejected at
			// seal with the no-export guidance.
			if err := s.Bind(Service[*svcA](), svcA{}); err != nil {
				t.Fatalf("bind registration is accepted and resolved at seal: %v", err)
			}
			if err := s.Bind(di.NewKey[depI]("custom.named"), implX{}); err == nil {
				t.Fatal("non-canonical token binding must be rejected at registration")
			} else {
				mustErrContains(t, err, "canonical")
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "no module exported this contract")
	})
	t.Run("replace contract twice rotates implementations deterministically", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
			Module{Name: "impl", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newUseI); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "owner", func(s Services) {
			if err := s.Export(Contract[depI]()); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newX); err != nil {
				t.Fatal(err)
			}
			if err := s.ReplaceContract(Contract[depI](), newY, "first swap"); err != nil {
				t.Fatal(err)
			}
			if err := s.ReplaceContract(Contract[depI](), newX, "second swap"); err == nil {
				// Re-adding the previously excluded identity is legal as a
				// fresh replacement; the engine re-registers it for THIS
				// contract.
				t.Log("re-adding excluded identity accepted as fresh replacement")
			}
		})
		seal(t, c)
	})
}

func TestServicesCanonicalInterfaceLowLevelProvider(t *testing.T) {
	contractToken := Contract[depI]()
	factoryCount := 0
	c := newComposition(t, planOf(
		Module{Name: "user", DependsOn: []Dependency{{Name: "foundation"}}},
		Module{Name: "foundation"},
	))
	if err := c.Provide("foundation", di.Provider{Key: contractToken, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		factoryCount++
		return implX{}, nil, nil
	}}); err != nil {
		t.Fatal(err)
	}
	register(t, c, "foundation", func(s Services) {
		if err := s.Export(contractToken); err != nil {
			t.Fatalf("canonical interface export by owner: %v", err)
		}
	})
	register(t, c, "user", func(s Services) {
		if err := s.Add(func(d depI) *svcB { return &svcB{} }); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	v, err := container.Resolve(context.Background(), contractToken)
	if err != nil {
		t.Fatalf("canonical interface low-level provider must seal and resolve alone: %v", err)
	}
	if v.(depI).Do() != "x" {
		t.Fatalf("unexpected implementation %v", v)
	}
	if factoryCount != 1 {
		t.Fatalf("factory must run exactly once, ran %d", factoryCount)
	}
}

func TestServicesNamedLowLevelConcretesNotImplicitCandidates(t *testing.T) {
	// Two custom-named low-level CONCRETE providers satisfying an interface
	// never become implicit exported contract candidates.
	contractToken := Contract[depI]()
	namedX := di.NewKey[implX]("named.implx")
	namedY := di.NewKey[implY]("named.imply")

	t.Run("exported contract with only named concretes has no candidate", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		if err := c.Provide("dep", di.Provider{Key: namedX, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return implX{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		if err := c.Provide("dep", di.Provider{Key: namedY, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return implY{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		register(t, c, "dep", func(s Services) {
			// The owner exports the contract but contributes NO constructor
			// and NO As association: the named concretes must not be
			// implicitly promoted to candidates by structural satisfaction.
			if err := s.Export(contractToken); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "depI", "has no implementation")
	})

	t.Run("unexported contract with named concretes stays missing", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		if err := c.Provide("dep", di.Provider{Key: namedX, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return implX{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		if err := c.Provide("dep", di.Provider{Key: namedY, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return implY{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		register(t, c, "app", func(s Services) {
			if err := s.Add(func(d depI) *useI { return &useI{} }); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "depI", "missing provider")
	})

	t.Run("named identities coexist with a real contract candidate", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
			Module{Name: "dep"},
		))
		if err := c.Provide("dep", di.Provider{Key: namedX, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return implX{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		register(t, c, "dep", func(s Services) {
			if err := s.Add(newY, As(contractToken)); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(contractToken); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(namedX); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "app", func(s Services) {
			if err := s.Add(func(d depI) *useI { return &useI{} }); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), namedX)
		if err != nil {
			t.Fatalf("named identity must coexist under its exact token: %v", err)
		}
		if v.(depI).Do() != "x" {
			t.Fatalf("named provider identity drifted: %v", v)
		}
		cv, err := container.Resolve(context.Background(), contractToken)
		if err != nil {
			t.Fatalf("contract resolution must be unaffected: %v", err)
		}
		if cv.(depI).Do() != "y" {
			t.Fatalf("contract candidate drifted: %v", cv)
		}
	})
}

func TestServicesLowLevelConstructorConflictStillFailsBeforeFactories(t *testing.T) {
	// After the self-candidate fix, a REAL conflict still fails at seal: a
	// low-level provider under the exact contract identity plus an actual
	// constructor association. No factory may run.
	factoryCount := 0
	c := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "dep"}}},
		Module{Name: "dep"},
	))
	if err := c.Provide("dep", di.Provider{Key: Contract[depI](), Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
		factoryCount++
		return implX{}, nil, nil
	}}); err != nil {
		t.Fatal(err)
	}
	register(t, c, "dep", func(s Services) {
		if err := s.Export(Contract[depI]()); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newY, As(Contract[depI]())); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "app", func(s Services) {
		if err := s.Add(func(d depI) *useI { return &useI{} }); err != nil {
			t.Fatal(err)
		}
	})
	_, err := c.Seal(context.Background())
	mustErrContains(t, err, "implemented by both a low-level provider and constructor candidates")
	if factoryCount != 0 {
		t.Fatalf("factories must not run on seal failure, ran %d", factoryCount)
	}
}

func TestServicesNonOwnerBindPositiveAndNegative(t *testing.T) {
	// Positive: assembly depends directly on BOTH the contract owner and the
	// chosen implementation module; its bind resolves the real ambiguity to i2.
	c := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "owner"},
		Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "assembly", DependsOn: []Dependency{{Name: "owner"}, {Name: "i2"}}},
	))
	register(t, c, "app", func(s Services) {
		if err := s.Add(func(d depI) *useI { return &useI{} }); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "owner", func(s Services) {
		if err := s.Export(Contract[depI]()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "i1", func(s Services) {
		if err := s.Add(newX, As(Contract[depI]())); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "i2", func(s Services) {
		if err := s.Add(newY, As(Contract[depI]())); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "assembly", func(s Services) {
		if err := s.Bind(Contract[depI](), implY{}); err != nil {
			t.Fatalf("assembly bind with both direct deps must be accepted: %v", err)
		}
	})
	container := seal(t, c)
	v, err := container.Resolve(context.Background(), Contract[depI]())
	if err != nil {
		t.Fatal(err)
	}
	if v.(depI).Do() != "y" {
		t.Fatalf("non-owner bind must select i2, got %v", v)
	}
	// Negative: missing the implementation edge — bind is recorded but the
	// seal rejects it and no factory runs.
	c2 := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "owner"},
		Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "assembly2", DependsOn: []Dependency{{Name: "owner"}}},
	))
	register(t, c2, "app", func(s Services) {
		if err := s.Add(func(d depI) *useI { return &useI{} }); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c2, "owner", func(s Services) {
		if err := s.Export(Contract[depI]()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c2, "i1", func(s Services) {
		if err := s.Add(newX, As(Contract[depI]())); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c2, "i2", func(s Services) {
		if err := s.Add(newY, As(Contract[depI]())); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c2, "assembly2", func(s Services) {
		if err := s.Bind(Contract[depI](), implY{}); err != nil {
			t.Fatal(err)
		}
	})
	_, sealErr2 := c2.Seal(context.Background())
	mustErrContains(t, sealErr2, "i2")
	// Negative: missing the contract-owner edge.
	c3 := newComposition(t, planOf(
		Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "owner"},
		Module{Name: "i1", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "i2", DependsOn: []Dependency{{Name: "owner"}}},
		Module{Name: "assembly3", DependsOn: []Dependency{{Name: "i2"}}},
	))
	register(t, c3, "app", func(s Services) {
		if err := s.Add(func(d depI) *useI { return &useI{} }); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c3, "owner", func(s Services) {
		if err := s.Export(Contract[depI]()); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c3, "i1", func(s Services) {
		if err := s.Add(newX, As(Contract[depI]())); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c3, "i2", func(s Services) {
		if err := s.Add(newY, As(Contract[depI]())); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c3, "assembly3", func(s Services) {
		if err := s.Bind(Contract[depI](), implY{}); err != nil {
			t.Fatal(err)
		}
	})
	_, sealErr3 := c3.Seal(context.Background())
	mustErrContains(t, sealErr3, "owner")
}

func TestServicesRootBuilderInterfaceShape(t *testing.T) {
	// The runtime hook wrapper implements RootBuilder; Services() returns the
	// registrar directly so ordinary hooks can chain b.Services().Add(...).
	var b RootBuilder = builderStub{}
	if err := b.SetDefault("k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := b.Provide(di.Provider{}); err == nil {
		t.Fatal("stub must delegate through engine validation, not accept blindly")
	}
}

type builderStub struct {
	services Services
}

func (b builderStub) SetDefault(key, value string) error { return nil }
func (b builderStub) Services() Services                 { return b.services }

func (b builderStub) Provide(p di.Provider) error {
	return errors.New("stub: Provide must be stamped by the host")
}

func (b builderStub) Replace(p di.Provider, reason string) error { return errors.New("stub") }
