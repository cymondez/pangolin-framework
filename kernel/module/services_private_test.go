package module

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/cymondez/pangolin-framework/kernel/di"
)

// ---- owner-private interface fixtures ----

type privStoreI interface {
	Find(id string) (string, bool)
}

type privStore struct{ marker string }

func (s *privStore) Find(id string) (string, bool) { return s.marker, true }

func newPrivStore() *privStore { return &privStore{marker: "local"} }

type privUsecase struct{ Store privStoreI }

func newPrivUsecase(s privStoreI) *privUsecase { return &privUsecase{Store: s} }

// Value-type implementers for ambiguity cases.
type privStoreX struct{ marker string }

func (s privStoreX) Find(id string) (string, bool) { return s.marker, true }

func newPrivStoreX() privStoreX { return privStoreX{marker: "x"} }

type privStoreY struct{ marker string }

func (s privStoreY) Find(id string) (string, bool) { return s.marker, true }

func newPrivStoreY() privStoreY { return privStoreY{marker: "y"} }

type privUsecaseX struct{ Store privStoreI }

func newPrivUsecaseX(s privStoreI) *privUsecaseX { return &privUsecaseX{Store: s} }

// Per-module fixtures: the SAME Go interface used privately by two modules.
type privStoreA struct{ marker string }

func (s privStoreA) Find(id string) (string, bool) { return s.marker, true }

func newPrivStoreA() privStoreA { return privStoreA{marker: "A"} }

type privUsecaseA struct{ Store privStoreI }

func newPrivUsecaseA(s privStoreI) *privUsecaseA { return &privUsecaseA{Store: s} }

type privStoreB struct{ marker string }

func (s privStoreB) Find(id string) (string, bool) { return s.marker, true }

func newPrivStoreB() privStoreB { return privStoreB{marker: "B"} }

type privUsecaseB struct{ Store privStoreI }

func newPrivUsecaseB(s privStoreI) *privUsecaseB { return &privUsecaseB{Store: s} }

// Foreign and local fixtures.
type privStoreForeign struct{}

func (s privStoreForeign) Find(id string) (string, bool) { return "foreign", true }

var privForeignFactoryCalls int

func newPrivStoreForeign() privStoreForeign {
	privForeignFactoryCalls++
	return privStoreForeign{}
}

type privUsecaseClient struct{ Store privStoreI }

func newPrivUsecaseClient(s privStoreI) *privUsecaseClient { return &privUsecaseClient{Store: s} }

type privStoreLocal struct{ marker string }

func (s privStoreLocal) Find(id string) (string, bool) { return s.marker, true }

func newPrivStoreLocal() privStoreLocal { return privStoreLocal{marker: "local"} }

type privStorePublic struct{ marker string }

func (s privStorePublic) Find(id string) (string, bool) { return s.marker, true }

func newPrivStorePublic() privStorePublic { return privStorePublic{marker: "public"} }

// Scoped fixtures: Operation lifetime with cleanup, and a singleton with cleanup.
type privScopedStore struct{ n int }

var privScopedCalls, privScopedCleanups int

func (s *privScopedStore) Find(id string) (string, bool) { return "scoped", true }

func newPrivScopedStore() (*privScopedStore, func(context.Context) error, error) {
	privScopedCalls++
	s := &privScopedStore{n: privScopedCalls}
	return s, func(context.Context) error { privScopedCleanups++; return nil }, nil
}

type privSingletonStore struct{}

var privSingletonCleanups int

func (s *privSingletonStore) Find(id string) (string, bool) { return "singleton", true }

func newPrivSingletonStore() (*privSingletonStore, func(context.Context) error, error) {
	return &privSingletonStore{}, func(context.Context) error { privSingletonCleanups++; return nil }, nil
}

// Laundering fixture: Operation-lifetime implementation.
type privStoreOp struct{}

func (s privStoreOp) Find(id string) (string, bool) { return "op", true }

func newPrivStoreOp() privStoreOp { return privStoreOp{} }

// Cycle fixtures: the store constructor consumes the usecase, and the usecase
// consumes the store's private interface.
type privUsecaseCyclic struct{ Store privStoreI }

func newPrivUsecaseCyclic(s privStoreI) *privUsecaseCyclic { return &privUsecaseCyclic{Store: s} }

type privStoreCyclic struct{}

func (s privStoreCyclic) Find(id string) (string, bool) { return "cyclic", true }

func newPrivStoreCyclic(u *privUsecaseCyclic) privStoreCyclic { return privStoreCyclic{} }

func privAliasTestToken(owner string) di.Token {
	return di.NewKey[privStoreI](canonicalTokenName(reflect.TypeFor[privStoreI]()) + "\x00private\x00" + owner)
}

// The headline defect: one module, two ordinary Add calls, no Export, no
// low-level tokens, no provider declarations.
func TestPrivateInterfaceOwnerLocalComposesDirectly(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newPrivStore); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newPrivUsecase); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	ctx := context.Background()
	ucV, err := container.Resolve(ctx, Service[*privUsecase]())
	if err != nil {
		t.Fatalf("resolve usecase: %v", err)
	}
	uc := ucV.(*privUsecase)
	repo, ok := uc.Store.(*privStore)
	if !ok || repo.marker != "local" {
		t.Fatalf("usecase must receive the owner-private implementation, got %T %+v", uc.Store, uc.Store)
	}
	res, err := c.LifecycleResolver("app")
	if err != nil {
		t.Fatal(err)
	}
	storeV, err := res.Resolve(ctx, Contract[privStoreI]())
	if err != nil {
		t.Fatalf("lifecycle resolve of own private interface: %v", err)
	}
	if st, ok := storeV.(*privStore); !ok || st.marker != "local" {
		t.Fatalf("lifecycle resolver returned wrong private implementation: %v", storeV)
	}
	if _, err := res.Resolve(ctx, Service[*privUsecase]()); err != nil {
		t.Fatalf("lifecycle resolve of own usecase: %v", err)
	}
	scope, err := OpenScope(ctx, res)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close(ctx)
	if _, err := scope.Resolve(ctx, Contract[privStoreI]()); err != nil {
		t.Fatalf("scope resolve of own private interface: %v", err)
	}
	if _, err := scope.Resolve(ctx, Service[*privUsecase]()); err != nil {
		t.Fatalf("scope resolve of own usecase: %v", err)
	}
	if _, err := container.Resolve(ctx, Contract[privStoreI]()); err == nil {
		t.Fatal("the unexported canonical token must not be globally resolvable through the sealed container")
	} else {
		mustErrContains(t, err, "missing provider", "privStoreI")
	}
}

func TestPrivateInterfaceHiddenFromDirectDependent(t *testing.T) {
	t.Run("dependent cannot satisfy its own parameter with the provider's private implementation", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "lib"},
			Module{Name: "client", DependsOn: []Dependency{{Name: "lib"}}},
		))
		register(t, c, "lib", func(s Services) {
			if err := s.Add(newPrivStore); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "client", func(s Services) {
			if err := s.Add(newPrivUsecaseClient); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "client", "privStoreI", "missing provider")
		if strings.Contains(err.Error(), "local") {
			t.Fatalf("diagnostic must not leak the owner-private implementation: %v", err)
		}
	})
	t.Run("foreign compatible constructor is not a private candidate", func(t *testing.T) {
		privForeignFactoryCalls = 0
		c := newComposition(t, planOf(
			Module{Name: "provider"},
			Module{Name: "client", DependsOn: []Dependency{{Name: "provider"}}},
		))
		register(t, c, "provider", func(s Services) {
			if err := s.Add(newPrivStoreForeign); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "client", func(s Services) {
			if err := s.Add(newPrivUsecaseClient); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "client", "privStoreI", "missing provider")
		if strings.Contains(err.Error(), "foreign") {
			t.Fatalf("candidate list must not include foreign types: %v", err)
		}
		if privForeignFactoryCalls != 0 {
			t.Fatalf("factories must not run on failed seal, ran %d", privForeignFactoryCalls)
		}
	})
	t.Run("owner alias stays private after seal", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "lib"},
			Module{Name: "client", DependsOn: []Dependency{{Name: "lib"}}},
		))
		register(t, c, "lib", func(s Services) {
			if err := s.Add(newPrivStore); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		libRes, err := c.LifecycleResolver("lib")
		if err != nil {
			t.Fatal(err)
		}
		v, err := libRes.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("owner lifecycle resolve: %v", err)
		}
		if st, ok := v.(*privStore); !ok || st.marker != "local" {
			t.Fatalf("unexpected owner alias resolution: %v", v)
		}
		clientRes, err := c.LifecycleResolver("client")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := clientRes.Resolve(ctx, Contract[privStoreI]()); err == nil {
			t.Fatal("a direct dependent must not resolve the owner's private alias")
		} else {
			mustErrContains(t, err, "client", "missing provider")
		}
		if _, err := container.Resolve(ctx, Service[*privUsecase]()); err != nil {
			t.Fatalf("owner usecase still resolves: %v", err)
		}
	})
}

func TestPrivateInterfaceSharedTypeIndependentPerModule(t *testing.T) {
	t.Run("two modules reuse the same Go interface privately", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "a"}, Module{Name: "b"}))
		register(t, c, "a", func(s Services) {
			if err := s.Add(newPrivStoreA); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecaseA); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "b", func(s Services) {
			if err := s.Add(newPrivStoreB); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecaseB); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		av, err := container.Resolve(ctx, Service[*privUsecaseA]())
		if err != nil {
			t.Fatal(err)
		}
		bv, err := container.Resolve(ctx, Service[*privUsecaseB]())
		if err != nil {
			t.Fatal(err)
		}
		if got := av.(*privUsecaseA).Store.(privStoreA).marker; got != "A" {
			t.Fatalf("module a resolved %q", got)
		}
		if got := bv.(*privUsecaseB).Store.(privStoreB).marker; got != "B" {
			t.Fatalf("module b resolved %q", got)
		}
		aRes, _ := c.LifecycleResolver("a")
		bRes, _ := c.LifecycleResolver("b")
		if v, err := aRes.Resolve(ctx, Contract[privStoreI]()); err != nil {
			t.Fatal(err)
		} else if got := v.(privStoreA).marker; got != "A" {
			t.Fatalf("module a alias drifted: %q", got)
		}
		if v, err := bRes.Resolve(ctx, Contract[privStoreI]()); err != nil {
			t.Fatal(err)
		} else if got := v.(privStoreB).marker; got != "B" {
			t.Fatalf("module b alias drifted: %q", got)
		}
		if _, err := container.Resolve(ctx, Contract[privStoreI]()); err == nil {
			t.Fatal("the shared private interface must not gain a global canonical identity")
		}
	})
	t.Run("dependence on the other module does not leak its private alias", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "a"},
			Module{Name: "b", DependsOn: []Dependency{{Name: "a"}}},
		))
		register(t, c, "a", func(s Services) {
			if err := s.Add(newPrivStoreA); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecaseA); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "b", func(s Services) {
			if err := s.Add(newPrivStoreB); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecaseB); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		bv, err := container.Resolve(ctx, Service[*privUsecaseB]())
		if err != nil {
			t.Fatal(err)
		}
		if got := bv.(*privUsecaseB).Store.(privStoreB).marker; got != "B" {
			t.Fatalf("dependent module must resolve its own implementation, got %q", got)
		}
	})
}

func TestPrivateInterfaceAmbiguityAndBind(t *testing.T) {
	t.Run("ambiguous private interface fails before factories", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		xCalls, yCalls, useCalls := 0, 0, 0
		register(t, c, "app", func(s Services) {
			if err := s.Add(func() privStoreX { xCalls++; return privStoreX{marker: "x"} }); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(func() privStoreY { yCalls++; return privStoreY{marker: "y"} }); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(func(s privStoreI) *privUsecaseX { useCalls++; return &privUsecaseX{Store: s} }); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "ambiguous", "app", "privStoreI", "module=app type=module.privStoreX", "module=app type=module.privStoreY")
		if xCalls != 0 || yCalls != 0 || useCalls != 0 {
			t.Fatalf("factories must not run on failed seal (x=%d y=%d use=%d)", xCalls, yCalls, useCalls)
		}
	})
	t.Run("explicit bind selects an own candidate without exporting", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreX); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStoreY); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecaseX); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreY{}); err != nil {
				t.Fatalf("private bind: %v", err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		v, err := container.Resolve(ctx, Service[*privUsecaseX]())
		if err != nil {
			t.Fatal(err)
		}
		if got := v.(*privUsecaseX).Store.(privStoreY).marker; got != "y" {
			t.Fatalf("private bind selected %q", got)
		}
		if _, err := container.Resolve(ctx, Contract[privStoreI]()); err == nil {
			t.Fatal("the privately bound interface must stay unexported")
		}
	})
	t.Run("redundant private binding on a single candidate", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreX); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecaseX); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreX{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "redundant private binding")
	})
	t.Run("private bind must match an own candidate", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreX); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStoreY); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecaseX); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreForeign{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "does not match any candidate")
	})
}

func TestPrivateInterfaceReversedRegistrationOrder(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	// The consumer is registered first, in its own provenance window, and the
	// implementation arrives afterwards; inference happens at Seal.
	register(t, c, "app", func(s Services) {
		if err := s.Add(newPrivUsecase); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "app", func(s Services) {
		if err := s.Add(newPrivStore); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	v, err := container.Resolve(context.Background(), Service[*privUsecase]())
	if err != nil {
		t.Fatal(err)
	}
	if got := v.(*privUsecase).Store.(*privStore).marker; got != "local" {
		t.Fatalf("reversed order resolved %q", got)
	}
}

func TestPrivateInterfacePublicOwnerAuthority(t *testing.T) {
	t.Run("direct dependency resolves through the public contract", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app", DependsOn: []Dependency{{Name: "owner"}}},
			Module{Name: "owner"},
		))
		register(t, c, "owner", func(s Services) {
			if err := s.Add(newPrivStorePublic); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(Contract[privStoreI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreLocal); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		v, err := container.Resolve(ctx, Service[*privUsecase]())
		if err != nil {
			t.Fatal(err)
		}
		if got := v.(*privUsecase).Store.(privStorePublic).marker; got != "public" {
			t.Fatalf("public contract must win over the local match, got %q", got)
		}
		cv, err := container.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatal(err)
		}
		if got := cv.(privStorePublic).marker; got != "public" {
			t.Fatalf("contract resolution drifted: %q", got)
		}
		lv, err := container.Resolve(ctx, Service[privStoreLocal]())
		if err != nil {
			t.Fatalf("the local candidate stays resolvable as a plain service: %v", err)
		}
		if got := lv.(privStoreLocal).marker; got != "local" {
			t.Fatalf("local service drifted: %q", got)
		}
	})
	t.Run("missing owner edge is not shielded by a local candidate", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app"},
			Module{Name: "owner"},
		))
		register(t, c, "owner", func(s Services) {
			if err := s.Add(newPrivStorePublic); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(Contract[privStoreI]()); err != nil {
				t.Fatal(err)
			}
		})
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreLocal); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "app", "privStoreI", "owner", "declare a direct dependency")
	})
}

func TestPrivateInterfaceLifecycleAndScopes(t *testing.T) {
	t.Run("operation lifetime requires an explicit scope", func(t *testing.T) {
		privScopedCalls, privScopedCleanups = 0, 0
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivScopedStore, Operation()); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		seal(t, c)
		ctx := context.Background()
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.Resolve(ctx, Contract[privStoreI]()); err == nil {
			t.Fatal("operation-lifetime private alias must require a scope")
		} else {
			mustErrContains(t, err, "operation provider requires scope")
		}
		scope, err := OpenScope(ctx, res)
		if err != nil {
			t.Fatal(err)
		}
		v1, err := scope.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("scope resolve: %v", err)
		}
		v1b, err := scope.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatal(err)
		}
		if v1 != v1b {
			t.Fatal("operation instances must be cached per scope")
		}
		ucV, err := scope.Resolve(ctx, Service[*privUsecase]())
		if err != nil {
			t.Fatalf("scope resolve usecase: %v", err)
		}
		if ucV.(*privUsecase).Store != v1 {
			t.Fatal("usecase must receive the scope-cached operation instance")
		}
		if err := scope.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if privScopedCleanups != 1 {
			t.Fatalf("cleanup must run once per scope, ran %d", privScopedCleanups)
		}
		scope2, err := OpenScope(ctx, res)
		if err != nil {
			t.Fatal(err)
		}
		v2, err := scope2.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatal(err)
		}
		if v2 == v1 {
			t.Fatal("a second scope must construct a fresh operation instance")
		}
		if err := scope2.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if privScopedCleanups != 2 || privScopedCalls != 2 {
			t.Fatalf("unexpected operation counters: calls=%d cleanups=%d", privScopedCalls, privScopedCleanups)
		}
	})
	t.Run("singleton private store is shared across scopes until container close", func(t *testing.T) {
		privSingletonCleanups = 0
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivSingletonStore, Singleton()); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		scope1, err := OpenScope(ctx, res)
		if err != nil {
			t.Fatal(err)
		}
		v1, err := scope1.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatal(err)
		}
		scope2, err := OpenScope(ctx, res)
		if err != nil {
			t.Fatal(err)
		}
		v2, err := scope2.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatal(err)
		}
		if v1 != v2 {
			t.Fatal("singleton private implementation must be shared across scopes")
		}
		if err := scope1.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if err := scope2.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if privSingletonCleanups != 0 {
			t.Fatalf("scope close must not dispose the singleton, ran %d", privSingletonCleanups)
		}
		if err := container.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if privSingletonCleanups != 1 {
			t.Fatalf("container close must dispose the singleton once, ran %d", privSingletonCleanups)
		}
	})
}

func TestPrivateInterfaceAliasIdentityFence(t *testing.T) {
	aliasToken := privAliasTestToken("app")
	t.Run("provider identity in the engine-managed namespace is rejected", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		err := c.Provide("app", di.Provider{Key: aliasToken, Lifetime: di.Singleton,
			Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return privStoreX{}, nil, nil }})
		mustErrContains(t, err, "private alias namespace")
	})
	t.Run("dependencies pointing at an alias identity are rejected at seal", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "app"}, Module{Name: "lib"}))
		err := c.Provide("lib", di.Provider{Key: di.NewKey[privStoreX]("plain.lib.store"), Lifetime: di.Singleton,
			DependsOn: []di.Token{aliasToken},
			Factory: func(ctx context.Context, r di.Resolver) (any, di.Cleanup, error) {
				if _, rerr := r.Resolve(ctx, aliasToken); rerr != nil {
					return nil, nil, rerr
				}
				return privStoreX{}, nil, nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Seal(context.Background())
		mustErrContains(t, err, "private alias identity")
	})
	t.Run("lifecycle resolver and scope refuse alias identities", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStore); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		seal(t, c)
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if _, err := res.Resolve(ctx, aliasToken); err == nil {
			t.Fatal("alias identities must not be resolvable as tokens")
		} else {
			mustErrContains(t, err, "private alias identity")
		}
		scope, err := OpenScope(ctx, res)
		if err != nil {
			t.Fatal(err)
		}
		defer scope.Close(ctx)
		if _, err := scope.Resolve(ctx, aliasToken); err == nil {
			t.Fatal("alias identities must not be resolvable through a scope")
		} else {
			mustErrContains(t, err, "private alias identity")
		}
	})
	t.Run("custom-named token binding stays rejected", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			err := s.Bind(di.NewKey[privStoreI]("custom.named"), privStoreX{})
			if err == nil {
				t.Fatal("non-canonical binding must be rejected")
			}
			mustErrContains(t, err, "canonical")
		})
	})
}

func TestPrivateInterfaceDanglingBindings(t *testing.T) {
	t.Run("binding without a consuming constructor resolves privately", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreX); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreX{}); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		v, err := res.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("bind-only private interface must resolve without Export: %v", err)
		}
		if st, ok := v.(privStoreX); !ok || st.marker != "x" {
			t.Fatalf("bind-only resolution drifted: %v", v)
		}
		if _, err := container.Resolve(ctx, Contract[privStoreI]()); err == nil {
			t.Fatal("the bind-only alias must stay unexported in the global container")
		} else {
			mustErrContains(t, err, "missing provider")
		}
	})
	t.Run("binding with an exact own registration is redundant", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		err := c.Provide("app", di.Provider{Key: Contract[privStoreI](), Lifetime: di.Singleton,
			Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return privStoreX{}, nil, nil }})
		if err != nil {
			t.Fatal(err)
		}
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreX{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err = c.Seal(context.Background())
		mustErrContains(t, err, "redundant", "exact")
	})
	t.Run("bind-then-export promotes the binding to the public rule", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "owner"},
			Module{Name: "consumer", DependsOn: []Dependency{{Name: "owner"}}},
		))
		register(t, c, "owner", func(s Services) {
			if err := s.Add(newPrivStoreX); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStoreY); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreY{}); err != nil {
				t.Fatal(err)
			}
			if err := s.Export(Contract[privStoreI]()); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Contract[privStoreI]())
		if err != nil {
			t.Fatalf("promoted binding must resolve the public contract: %v", err)
		}
		if got := v.(privStoreY).marker; got != "y" {
			t.Fatalf("promoted binding selected %q", got)
		}
	})
	t.Run("foreign exact registration blocks a private binding", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "provider"},
			Module{Name: "client", DependsOn: []Dependency{{Name: "provider"}}},
		))
		err := c.Provide("provider", di.Provider{Key: Contract[privStoreI](), Lifetime: di.Singleton,
			Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return privStoreX{}, nil, nil }})
		if err != nil {
			t.Fatal(err)
		}
		register(t, c, "client", func(s Services) {
			if err := s.Add(newPrivUsecaseClient); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreX{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err = c.Seal(context.Background())
		mustErrContains(t, err, "client", "privStoreI", "provider")
	})
}

func TestPrivateInterfaceReplaceBinding(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newPrivStoreX); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newPrivStoreY); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newPrivUsecaseX); err != nil {
			t.Fatal(err)
		}
		if err := s.Bind(Contract[privStoreI](), privStoreY{}); err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceBinding(Contract[privStoreI](), privStoreY{}, ""); err == nil {
			t.Fatal("empty reason must be rejected")
		}
		if err := s.ReplaceBinding(Contract[privStoreI](), privStoreX{}, "rotate private selection"); err != nil {
			t.Fatalf("ReplaceBinding: %v", err)
		}
	})
	c2 := newComposition(t, singleModule("app"))
	register(t, c2, "app", func(s Services) {
		if err := s.Add(newPrivStoreX); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newPrivUsecaseX); err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceBinding(Contract[privStoreI](), privStoreX{}, "no binding yet"); err == nil {
			t.Fatal("ReplaceBinding without an existing private binding must be rejected")
		}
	})
	container := seal(t, c)
	v, err := container.Resolve(context.Background(), Service[*privUsecaseX]())
	if err != nil {
		t.Fatal(err)
	}
	if got := v.(*privUsecaseX).Store.(privStoreX).marker; got != "x" {
		t.Fatalf("ReplaceBinding did not select x, got %q", got)
	}
	if log := strings.Join(c.ReplacementLog(), "|"); !strings.Contains(log, "rotate private selection") {
		t.Fatalf("private binding replacement missing from provenance log: %v", log)
	}
}

func TestPrivateInterfaceLaunderingAndCycle(t *testing.T) {
	t.Run("singleton capturing an operation through a private alias", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivUsecase, Singleton()); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStoreOp, Operation()); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "captures operation", "privUsecase", "app")
	})
	t.Run("transient capture is legal and requires a scope at resolve", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStoreOp, Operation()); err != nil {
				t.Fatal(err)
			}
		})
		seal(t, c)
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.Resolve(context.Background(), Service[*privUsecase]()); err == nil {
			t.Fatal("transient capturing an operation service must require a scope")
		} else {
			mustErrContains(t, err, "operation provider requires scope")
		}
	})
	t.Run("cycle through a private alias", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		useCalls, storeCalls := 0, 0
		register(t, c, "app", func(s Services) {
			if err := s.Add(func(s privStoreI) *privUsecaseCyclic { useCalls++; return &privUsecaseCyclic{Store: s} }); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(func(u *privUsecaseCyclic) privStoreCyclic { storeCalls++; return privStoreCyclic{} }); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		mustErrContains(t, err, "provider cycle")
		if useCalls != 0 || storeCalls != 0 {
			t.Fatalf("factories must not run on failed seal (use=%d store=%d)", useCalls, storeCalls)
		}
	})
}
