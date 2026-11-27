// CORE-PRIVATE review follow-up reproduction tests for the four private
// resolution gaps: As-contributing constructors excluded from own private
// inference, foreign unexported canonical entries shadowing own inference,
// bind-only private interfaces unresolvable from Start/Stop/OpenScope, and
// low-level providers bypassing the guarded alias translation.
package module

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/pangolin-framework/pangolin/kernel/di"
)

// ---- gap 1 fixtures: one concrete serving a public contract and a private interface ----

type dualJ interface{ Do() string }

type dualStore struct{ marker string }

func (s *dualStore) Find(id string) (string, bool) { return s.marker, true }
func (s *dualStore) Do() string {
	return s.marker
}

func newDualStore() *dualStore { return &dualStore{marker: "dual"} }

type srcPrivI interface{ Serve() string }

type srcImpl struct{ marker string }

func (s srcImpl) Serve() string { return s.marker }

func newSrcImpl() srcImpl { return srcImpl{marker: "src-priv"} }

type dualStoreB struct{ marker string }

func (s *dualStoreB) Find(id string) (string, bool) { return s.marker, true }
func (s *dualStoreB) Do() string                    { return s.marker }

func newDualStoreB(p srcPrivI) *dualStoreB { return &dualStoreB{marker: "dual-b:" + p.Serve()} }

// gap 4 fixtures: a low-level gate consuming the owner-private canonical token.

type privGate struct{ Store privStoreI }

var privGateCalls, privGateUndeclaredCalls int

func newPrivGateProvider(count *int, deps []di.Token, key di.Token) di.Provider {
	return di.Provider{
		Key: key, Lifetime: di.Singleton, DependsOn: deps,
		Factory: func(ctx context.Context, r di.Resolver) (any, di.Cleanup, error) {
			*count++
			v, err := r.Resolve(ctx, deps[0])
			if err != nil {
				return nil, nil, err
			}
			return &privGate{Store: v.(privStoreI)}, nil, nil
		},
	}
}

func keyPrivGateCustom() di.Token {
	return di.NewKey[*privGate]("app.custom-gate")
}

type namedStore struct{}

func (s namedStore) Find(id string) (string, bool) { return "named", true }

func privateMustError(t *testing.T, err error, parts ...string) {
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

func privateMustNotContain(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, p := range forbidden {
		if strings.Contains(err.Error(), p) {
			t.Fatalf("error %q must not contain %q", err.Error(), p)
		}
	}
}

// (1) An own constructor contributing PUBLIC J must still satisfy a DIFFERENT
// own PRIVATE I; As must not make the service inaccessible to its registering
// module, and public-I authority must still win.
func TestPrivateAsContributionStillServesOwnPrivateInterface(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newDualStore, As(Contract[dualJ]())); err != nil {
			t.Fatal(err)
		}
		if err := s.Export(Contract[dualJ]()); err != nil {
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
	if got := ucV.(*privUsecase).Store.(*dualStore).marker; got != "dual" {
		t.Fatalf("own private interface must resolve the As-contributing concrete, got %q", got)
	}
	jV, err := container.Resolve(ctx, Contract[dualJ]())
	if err != nil {
		t.Fatalf("public contract resolution: %v", err)
	}
	if got := jV.(*dualStore).marker; got != "dual" {
		t.Fatalf("public contract drifted: %q", got)
	}
	if _, err := container.Resolve(ctx, Contract[privStoreI]()); err == nil {
		t.Fatal("the private interface must stay unexported")
	} else {
		privateMustError(t, err, "missing provider")
	}
	res, err := c.LifecycleResolver("app")
	if err != nil {
		t.Fatal(err)
	}
	storeV, err := res.Resolve(ctx, Contract[privStoreI]())
	if err != nil {
		t.Fatalf("lifecycle resolve of own private interface: %v", err)
	}
	if got := storeV.(*dualStore).marker; got != "dual" {
		t.Fatalf("lifecycle alias drifted: %q", got)
	}
}

func TestPrivateAsContributionSurvivesContractReplacement(t *testing.T) {
	c := newComposition(t, planOf(
		Module{Name: "owner"},
		Module{Name: "source", DependsOn: []Dependency{{Name: "owner"}}},
	))
	register(t, c, "owner", func(s Services) {
		if err := s.Add(newDualStore, As(Contract[dualJ]())); err != nil {
			t.Fatal(err)
		}
		if err := s.Export(Contract[dualJ]()); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newPrivUsecase); err != nil {
			t.Fatal(err)
		}
	})
	register(t, c, "source", func(s Services) {
		if err := s.Add(newSrcImpl); err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceContract(Contract[dualJ](), newDualStoreB, "swap implementation source"); err != nil {
			t.Fatalf("ReplaceContract: %v", err)
		}
	})
	container := seal(t, c)
	ctx := context.Background()
	jV, err := container.Resolve(ctx, Contract[dualJ]())
	if err != nil {
		t.Fatalf("replaced contract resolution: %v", err)
	}
	if got := jV.(*dualStoreB).marker; got != "dual-b:src-priv" {
		t.Fatalf("replacement implementation drifted: %q", got)
	}
	ucV, err := container.Resolve(ctx, Service[*privUsecase]())
	if err != nil {
		t.Fatalf("owner usecase after replacement: %v", err)
	}
	if got := ucV.(*privUsecase).Store.(*dualStore).marker; got != "dual" {
		t.Fatalf("the original contract owner consumption must keep working, got %q", got)
	}
	srcRes, err := c.LifecycleResolver("source")
	if err != nil {
		t.Fatal(err)
	}
	srcV, err := srcRes.Resolve(ctx, Contract[srcPrivI]())
	if err != nil {
		t.Fatalf("source private dependency must stay source-scoped: %v", err)
	}
	if got := srcV.(srcImpl).marker; got != "src-priv" {
		t.Fatalf("source private alias drifted: %q", got)
	}
	ownerRes, _ := c.LifecycleResolver("owner")
	if _, err := ownerRes.Resolve(ctx, Contract[srcPrivI]()); err == nil {
		t.Fatal("the owner must not reach the source module's private alias")
	} else {
		privateMustNotContain(t, err, "src-priv")
	}
	if log := strings.Join(c.ReplacementLog(), "|"); !strings.Contains(log, "swap implementation source") {
		t.Fatalf("replacement provenance missing: %v", log)
	}
}

func TestPrivateAsToUnexportedContractStaysPrivatelyAccessible(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newPrivStore, As(Contract[privStoreI]())); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newPrivUsecase); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	ucV, err := container.Resolve(context.Background(), Service[*privUsecase]())
	if err != nil {
		t.Fatalf("As must not make the service inaccessible to its registering module: %v", err)
	}
	if got := ucV.(*privUsecase).Store.(*privStore).marker; got != "local" {
		t.Fatalf("private inference drifted: %q", got)
	}
}

func TestPrivateAsPublicAuthorityStillWins(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newPrivStorePublic, As(Contract[privStoreI]())); err != nil {
			t.Fatal(err)
		}
		if err := s.Export(Contract[privStoreI]()); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newPrivUsecase); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	ucV, err := container.Resolve(context.Background(), Service[*privUsecase]())
	if err != nil {
		t.Fatal(err)
	}
	if got := ucV.(*privUsecase).Store.(privStorePublic).marker; got != "public" {
		t.Fatalf("public-I authority must win, got %q", got)
	}
}

// (2) A foreign UNEXPORTED canonical interface entry must not shadow the own
// module's private inference, and the foreign owner keeps its exact identity.
func TestPrivateForeignUnexportedCanonicalDoesNotShadowOwnInference(t *testing.T) {
	c := newComposition(t, planOf(Module{Name: "app"}, Module{Name: "blib"}))
	if err := c.Provide("blib", di.Provider{Key: Contract[privStoreI](), Lifetime: di.Singleton,
		Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return privStoreForeign{}, nil, nil }}); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("own private inference must not be blocked by the foreign entry: %v", err)
	}
	if got := ucV.(*privUsecase).Store.(*privStore).marker; got != "local" {
		t.Fatalf("own inference drifted: %q", got)
	}
	appRes, err := c.LifecycleResolver("app")
	if err != nil {
		t.Fatal(err)
	}
	appV, err := appRes.Resolve(ctx, Contract[privStoreI]())
	if err != nil {
		t.Fatalf("app lifecycle resolve: %v", err)
	}
	if got := appV.(*privStore).marker; got != "local" {
		t.Fatalf("app private view drifted: %q", got)
	}
	blibRes, err := c.LifecycleResolver("blib")
	if err != nil {
		t.Fatal(err)
	}
	blibV, err := blibRes.Resolve(ctx, Contract[privStoreI]())
	if err != nil {
		t.Fatalf("blib keeps its own exact registration: %v", err)
	}
	if _, ok := blibV.(privStoreForeign); !ok {
		t.Fatalf("blib exact identity drifted: %T", blibV)
	}
	if _, err := container.Resolve(ctx, Service[*privUsecase]()); err != nil {
		t.Fatalf("app usecase still resolves: %v", err)
	}
}

func TestPrivateForeignCanonicalMissingCandidateMentionsOwner(t *testing.T) {
	c := newComposition(t, planOf(Module{Name: "app"}, Module{Name: "blib"}))
	if err := c.Provide("blib", di.Provider{Key: Contract[privStoreI](), Lifetime: di.Singleton,
		Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return privStoreForeign{}, nil, nil }}); err != nil {
		t.Fatal(err)
	}
	register(t, c, "app", func(s Services) {
		if err := s.Add(newPrivUsecase); err != nil {
			t.Fatal(err)
		}
	})
	_, err := c.Seal(context.Background())
	privateMustError(t, err, "app", "privStoreI", "missing provider", "blib")
	privateMustNotContain(t, err, "privStoreForeign")
}

// (3) Bind-only private interfaces work from Start/Stop/explicit OpenScope.
func TestPrivateBindOnlyLifecycleResolution(t *testing.T) {
	t.Run("unique implementation needs neither export nor consuming constructor nor bind", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStore); err != nil {
				t.Fatal(err)
			}
		})
		seal(t, c)
		ctx := context.Background()
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		v, err := res.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("unique own lifecycle-only interface must resolve without Export: %v", err)
		}
		if got := v.(*privStore).marker; got != "local" {
			t.Fatalf("dynamic selection drifted: %q", got)
		}
		scope, err := OpenScope(ctx, res)
		if err != nil {
			t.Fatal(err)
		}
		defer scope.Close(ctx)
		if _, err := scope.Resolve(ctx, Contract[privStoreI]()); err != nil {
			t.Fatalf("scope resolve of unique private interface: %v", err)
		}
	})
	t.Run("bind-only with multiple candidates selects through lifecycle and scope", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreX); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStoreY); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreY{}); err != nil {
				t.Fatalf("private bind without a consuming constructor: %v", err)
			}
		})
		seal(t, c)
		ctx := context.Background()
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		v, err := res.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("bind-only interface must resolve from Start/Stop hooks: %v", err)
		}
		if got := v.(privStoreY).marker; got != "y" {
			t.Fatalf("bind-only selection drifted: %q", got)
		}
		scope, err := OpenScope(ctx, res)
		if err != nil {
			t.Fatal(err)
		}
		defer scope.Close(ctx)
		sv, err := scope.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("scope resolve of bind-only interface: %v", err)
		}
		if got := sv.(privStoreY).marker; got != "y" {
			t.Fatalf("scope selection drifted: %q", got)
		}
		if _, err := c.Seal(context.Background()); err == nil {
			t.Fatal("seal is one-shot")
		}
	})
	t.Run("bind-only genuine ambiguity errors at resolve with private-bind guidance", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreX); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStoreY); err != nil {
				t.Fatal(err)
			}
		})
		// A low-level named provider must neither become an implicit
		// candidate nor change the ambiguity diagnostics on the dynamic
		// lifecycle-only resolve path.
		if err := c.Provide("app", di.Provider{Key: di.NewKey[privStoreI]("app.named-store"), Lifetime: di.Singleton,
			Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return privStoreForeign{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		seal(t, c)
		res, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		_, err = res.Resolve(context.Background(), Contract[privStoreI]())
		privateMustError(t, err, "ambiguous", "app", "privStoreI", "Bind", "Export is not a fix")
		privateMustNotContain(t, err, "export the contract explicitly", "privStoreForeign")
	})
	t.Run("bind-only with no own candidate fails at seal", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Bind(Contract[privStoreI](), privStoreForeign{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		privateMustError(t, err, "app", "privStoreI", "register an own implementation constructor")
		privateMustNotContain(t, err, "privStoreForeign")
	})
	t.Run("bind-only with a non-matching implementation fails at seal", func(t *testing.T) {
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreX); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStoreY); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreForeign{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		privateMustError(t, err, "app", "privStoreI", "does not match any candidate")
	})
	t.Run("bind-only with zero own candidates and a foreign exact entry reports the block", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "app"}, Module{Name: "blib"}))
		if err := c.Provide("blib", di.Provider{Key: Contract[privStoreI](), Lifetime: di.Singleton,
			Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return privStoreForeign{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		register(t, c, "app", func(s Services) {
			if err := s.Bind(Contract[privStoreI](), privStoreX{}); err != nil {
				t.Fatal(err)
			}
		})
		_, err := c.Seal(context.Background())
		privateMustError(t, err, "app", "privStoreI", "cannot apply", "blib")
		privateMustNotContain(t, err, "privStoreForeign")
	})
	t.Run("owner-private isolation of the bind-only alias", func(t *testing.T) {
		c := newComposition(t, planOf(
			Module{Name: "app"},
			Module{Name: "client", DependsOn: []Dependency{{Name: "app"}}},
		))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreX); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStoreY); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreX{}); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		clientRes, err := c.LifecycleResolver("client")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := clientRes.Resolve(ctx, Contract[privStoreI]()); err == nil {
			t.Fatal("the client must not resolve app's bind-only private alias")
		} else {
			privateMustError(t, err, "client", "missing provider")
			privateMustNotContain(t, err, "privStoreX")
		}
		appRes, _ := c.LifecycleResolver("app")
		v, err := appRes.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("owner resolve: %v", err)
		}
		if got := v.(privStoreX).marker; got != "x" {
			t.Fatalf("owner alias drifted: %q", got)
		}
		if _, err := container.Resolve(ctx, Contract[privStoreI]()); err == nil {
			t.Fatal("bind-only alias must stay unexported in the global container")
		}
	})
}

// (4) Validation and provisioning of alias-translated dependencies must agree:
// low-level factories with explicit canonical private DependsOn resolve
// through the guarded alias; custom named tokens stay exact; undeclared
// resolver access is denied; named providers never become candidates.
func TestPrivateAliasTranslatesLowLevelDeclaredDependency(t *testing.T) {
	t.Run("declared canonical private dependency resolves through the alias", func(t *testing.T) {
		privGateCalls = 0
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStore); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		if err := c.Provide("app", newPrivGateProvider(&privGateCalls, []di.Token{Contract[privStoreI]()}, Service[*privGate]())); err != nil {
			t.Fatal(err)
		}
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), Service[*privGate]())
		if err != nil {
			t.Fatalf("low-level factory canonical private dependency: %v", err)
		}
		if got := v.(*privGate).Store.(*privStore).marker; got != "local" {
			t.Fatalf("alias translation drifted: %q", got)
		}
		if privGateCalls != 1 {
			t.Fatalf("gate factory ran %d times", privGateCalls)
		}
	})
	t.Run("undeclared private resolver access is denied", func(t *testing.T) {
		privGateUndeclaredCalls = 0
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStore); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		key := Service[*privGate]()
		if err := c.Provide("app", di.Provider{Key: key, Lifetime: di.Transient,
			Factory: func(ctx context.Context, r di.Resolver) (any, di.Cleanup, error) {
				privGateUndeclaredCalls++
				v, err := r.Resolve(ctx, Contract[privStoreI]())
				if err != nil {
					return nil, nil, err
				}
				return &privGate{Store: v.(privStoreI)}, nil, nil
			}}); err != nil {
			t.Fatal(err)
		}
		container := seal(t, c)
		_, err := container.Resolve(context.Background(), key)
		privateMustError(t, err, "undeclared or expired factory dependency")
		if privGateUndeclaredCalls != 1 {
			t.Fatalf("undeclared access must fail inside the factory, ran %d", privGateUndeclaredCalls)
		}
	})
	t.Run("custom named dependency tokens stay exact", func(t *testing.T) {
		privGateCalls = 0
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStore); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		namedKey := di.NewKey[privStoreI]("named.store")
		if err := c.Provide("app", di.Provider{Key: namedKey, Lifetime: di.Singleton,
			Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return namedStore{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		if err := c.Provide("app", newPrivGateProvider(&privGateCalls, []di.Token{namedKey}, keyPrivGateCustom())); err != nil {
			t.Fatal(err)
		}
		container := seal(t, c)
		v, err := container.Resolve(context.Background(), keyPrivGateCustom())
		if err != nil {
			t.Fatalf("custom named dependency: %v", err)
		}
		store, ok := v.(*privGate).Store.(namedStore)
		if !ok {
			t.Fatalf("custom named dependency must resolve the exact named provider, got %T", v.(*privGate).Store)
		}
		if marker, _ := store.Find(""); marker != "named" {
			t.Fatalf("custom named dependency must not translate to the alias, got %q", marker)
		}
	})
	t.Run("missing own candidates with a declared private dep fails before factories", func(t *testing.T) {
		privGateCalls = 0
		c := newComposition(t, singleModule("app"))
		if err := c.Provide("app", newPrivGateProvider(&privGateCalls, []di.Token{Contract[privStoreI]()}, Service[*privGate]())); err != nil {
			t.Fatal(err)
		}
		_, err := c.Seal(context.Background())
		privateMustError(t, err, "app", "privStoreI", "missing provider")
		if privGateCalls != 0 {
			t.Fatalf("factories must not run on failed seal, ran %d", privGateCalls)
		}
	})
	t.Run("low-level named providers never become private candidates", func(t *testing.T) {
		privGateCalls = 0
		c := newComposition(t, singleModule("app"))
		if err := c.Provide("app", di.Provider{Key: Contract[privStoreI](), Lifetime: di.Singleton,
			Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return privStoreX{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		ucV, err := container.Resolve(context.Background(), Service[*privUsecase]())
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ucV.(*privUsecase).Store.(privStoreX); !ok {
			t.Fatalf("own exact registration must serve the parameter, got %T", ucV.(*privUsecase).Store)
		}
	})
	t.Run("mixed ctor and low-level consumers share one private alias", func(t *testing.T) {
		privGateCalls = 0
		c := newComposition(t, singleModule("app"))
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivSingletonStore, Singleton()); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivUsecase); err != nil {
				t.Fatal(err)
			}
		})
		if err := c.Provide("app", newPrivGateProvider(&privGateCalls, []di.Token{Contract[privStoreI]()}, Service[*privGate]())); err != nil {
			t.Fatal(err)
		}
		container := seal(t, c)
		ctx := context.Background()
		ucV, err := container.Resolve(ctx, Service[*privUsecase]())
		if err != nil {
			t.Fatal(err)
		}
		gateV, err := container.Resolve(ctx, Service[*privGate]())
		if err != nil {
			t.Fatal(err)
		}
		if ucV.(*privUsecase).Store != gateV.(*privGate).Store {
			t.Fatal("singleton private implementation identity must be shared across consumers")
		}
	})
	t.Run("bind-driven alias and foreign unexported entry compose without cross-leak", func(t *testing.T) {
		c := newComposition(t, planOf(Module{Name: "app"}, Module{Name: "blib"}))
		if err := c.Provide("blib", di.Provider{Key: Contract[privStoreI](), Lifetime: di.Singleton,
			Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return privStoreForeign{}, nil, nil }}); err != nil {
			t.Fatal(err)
		}
		register(t, c, "app", func(s Services) {
			if err := s.Add(newPrivStoreLocal); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(newPrivStorePublic); err != nil {
				t.Fatal(err)
			}
			if err := s.Bind(Contract[privStoreI](), privStoreLocal{}); err != nil {
				t.Fatal(err)
			}
		})
		container := seal(t, c)
		ctx := context.Background()
		appRes, err := c.LifecycleResolver("app")
		if err != nil {
			t.Fatal(err)
		}
		v, err := appRes.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("bind-driven alias with a foreign unexported entry: %v", err)
		}
		if got := v.(privStoreLocal).marker; got != "local" {
			t.Fatalf("bind selection drifted: %q", got)
		}
		blibRes, _ := c.LifecycleResolver("blib")
		blibV, err := blibRes.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("blib keeps its exact registration: %v", err)
		}
		if _, ok := blibV.(privStoreForeign); !ok {
			t.Fatalf("blib identity drifted: %T", blibV)
		}
		// The alias identity is separate from the exact registration
		// identity: the raw container still resolves blib's provider, while
		// app's restricted view resolves its own bind selection.
		globalV, err := container.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatalf("the exact registration identity keeps resolving: %v", err)
		}
		if _, ok := globalV.(privStoreForeign); !ok {
			t.Fatalf("global exact identity drifted: %T", globalV)
		}
		appRes2, _ := c.LifecycleResolver("app")
		v2, err := appRes2.Resolve(ctx, Contract[privStoreI]())
		if err != nil {
			t.Fatal(err)
		}
		if got := v2.(privStoreLocal).marker; got != "local" {
			t.Fatalf("second resolve drifted: %q", got)
		}
	})
}

// The alias translation must keep the canonical-key fence: alias identities
// remain unresolvable and canonical type identity is used for the table.
func TestPrivateCorrectionAliasIdentityStillEnforced(t *testing.T) {
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
	aliasToken := privAliasTestToken("app")
	if _, err := res.Resolve(context.Background(), aliasToken); err == nil {
		t.Fatal("alias identities must not be resolvable")
	} else {
		privateMustError(t, err, "private alias identity")
	}
	_ = reflect.TypeOf(aliasToken)
}

// Two constructors of one module declaring the same private parameter share a
// single alias: the parameter scan keeps one entry per canonical identity.
func TestPrivateCorrectionSharedParamUsesOneAlias(t *testing.T) {
	c := newComposition(t, singleModule("app"))
	register(t, c, "app", func(s Services) {
		if err := s.Add(newPrivSingletonStore, Singleton()); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newPrivUsecase); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(newPrivUsecaseClient); err != nil {
			t.Fatal(err)
		}
	})
	container := seal(t, c)
	ucV, err := container.Resolve(context.Background(), Service[*privUsecase]())
	if err != nil {
		t.Fatal(err)
	}
	clV, err := container.Resolve(context.Background(), Service[*privUsecaseClient]())
	if err != nil {
		t.Fatal(err)
	}
	if ucV.(*privUsecase).Store != clV.(*privUsecaseClient).Store {
		t.Fatal("both consumers must resolve through the same private alias identity")
	}
	if _, ok := ucV.(*privUsecase).Store.(*privSingletonStore); !ok {
		t.Fatalf("shared alias drifted: %T", ucV.(*privUsecase).Store)
	}
}
