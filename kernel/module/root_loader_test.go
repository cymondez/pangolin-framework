package module

import (
	"reflect"
	"strings"
	"testing"
)

type rootDataModule struct{ Loaded bool }

type rootAuthModule struct{}

func (m *rootAuthModule) DependsOn() []Ref { return []Ref{Of[rootDataModule]()} }

type rootNamedMetaModule struct{ Kinds []string }

func (m *rootNamedMetaModule) ModuleMetadata() Metadata {
	return Metadata{Name: "core", Version: "0.5.0", RequiredScopeKinds: []string{"org.organization"}}
}

func (m *rootNamedMetaModule) DependsOn() []Ref {
	return []Ref{Of[rootDataModule]().Requires(">=0.1.0 <9.0.0")}
}

type rootPanickyMetaModule struct{}

func (m *rootPanickyMetaModule) ModuleMetadata() Metadata { panic("boom") }

type rootPanickyDepsModule struct{}

func (m *rootPanickyDepsModule) DependsOn() []Ref { panic("dep boom") }

type rootBadRefNilModule struct{}

func (m *rootBadRefNilModule) DependsOn() []Ref {
	var refs []Ref
	refs = append(refs, Of[rootDataModule](), nil)
	return refs
}

type rootBadRefPointerModule struct{}

func (m *rootBadRefPointerModule) DependsOn() []Ref { return []Ref{Of[*rootDataModule]()} }

type rootBadRefUnnamedModule struct{}

func (m *rootBadRefUnnamedModule) DependsOn() []Ref {
	return []Ref{Of[struct{ A int }]()}
}

type rootBadRefInvalidConstraintModule struct{}

func (m *rootBadRefInvalidConstraintModule) DependsOn() []Ref {
	return []Ref{Of[rootDataModule]().Requires("^1.0.0")}
}

type rootSelfCycleModule struct{}

func (m *rootSelfCycleModule) DependsOn() []Ref { return []Ref{Of[rootSelfCycleModule]()} }

type rootCycleAModule struct{}

func (m *rootCycleAModule) DependsOn() []Ref { return []Ref{Of[rootCycleBModule]()} }

type rootCycleBModule struct{}

func (m *rootCycleBModule) DependsOn() []Ref { return []Ref{Of[rootCycleAModule]()} }

type rootCollideOneModule struct{}

func (m *rootCollideOneModule) ModuleMetadata() Metadata { return Metadata{Name: "dup"} }

type rootCollideTwoModule struct{}

func (m *rootCollideTwoModule) ModuleMetadata() Metadata { return Metadata{Name: "dup"} }

type rootInvalidVersionModule struct{}

func (m *rootInvalidVersionModule) ModuleMetadata() Metadata {
	return Metadata{Name: "bad", Version: "banana"}
}

type rootSharedConsumerOneModule struct{}

func (m *rootSharedConsumerOneModule) DependsOn() []Ref {
	return []Ref{Of[rootDataModule]().Requires("=9.9.9")}
}

type rootSharedConsumerTwoModule struct{}

func (m *rootSharedConsumerTwoModule) DependsOn() []Ref { return []Ref{Of[rootDataModule]()} }

func TestRootLoaderHappy(t *testing.T) {
	root := &rootAuthModule{}
	plan, err := LoadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Ordered) != 2 {
		t.Fatalf("want 2 planned modules, got %+v", plan.Plan.Ordered)
	}
	if plan.Plan.Layers[0][0] != qualName(rootDataModule{}) || len(plan.Plan.Layers[1]) != 1 || plan.Plan.Layers[1][0] != qualName(rootAuthModule{}) {
		t.Fatalf("unexpected layers: %v", plan.Plan.Layers)
	}
	if got, ok := plan.Instances[qualName(rootAuthModule{})]; !ok || got != any(root) {
		t.Fatalf("root pointer not retained: %v %v", got, ok)
	}
	dep, ok := plan.Instances[qualName(rootDataModule{})].(*rootDataModule)
	if !ok || dep == nil {
		t.Fatalf("dependency instance missing: %v", plan.Instances["module.rootDataModule"])
	}
	for _, m := range plan.Plan.Ordered {
		if m.Version != "0.1.0" {
			t.Fatalf("unexpected version %q for %s", m.Version, m.Name)
		}
	}
}

type diamondSharedModule struct{}

type diamondConsumerOne struct{}

func (m *diamondConsumerOne) DependsOn() []Ref { return []Ref{Of[diamondSharedModule]()} }

type diamondConsumerTwo struct{}

func (m *diamondConsumerTwo) DependsOn() []Ref { return []Ref{Of[diamondSharedModule]()} }

type diamondRoot struct{}

func (m *diamondRoot) DependsOn() []Ref {
	return []Ref{Of[diamondConsumerOne](), Of[diamondConsumerTwo]()}
}

func TestRootLoaderDiamondOnce(t *testing.T) {
	plan, err := LoadRoot(&diamondRoot{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Ordered) != 4 || len(plan.Instances) != 4 {
		t.Fatalf("diamond not deduplicated: %d ordered, %d instances", len(plan.Plan.Ordered), len(plan.Instances))
	}
	shared, ok := plan.Instances[qualName(diamondSharedModule{})].(*diamondSharedModule)
	if !ok || shared == nil {
		t.Fatalf("shared dependency missing: %v", plan.Instances)
	}
}

func TestRootLoaderMetadataDeclared(t *testing.T) {
	kinds := []string{"org.organization"}
	m := &rootNamedMetaModule{Kinds: kinds}
	plan, err := LoadRoot(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Ordered) != 2 {
		t.Fatalf("want 2 modules, got %+v", plan.Plan.Ordered)
	}
	core := plan.Plan.Ordered[1]
	if core.Name != "core" || core.Version != "0.5.0" || !reflect.DeepEqual(core.RequiredScopeKinds, []string{"org.organization"}) {
		t.Fatalf("declared metadata lost: %+v", core)
	}
	dep := plan.Plan.Ordered[0]
	if dep.Name != qualName(rootDataModule{}) {
		t.Fatalf("unexpected dep name %q", dep.Name)
	}
	// explicit range constraint preserved on the translated dependency
	var depOn []Dependency
	for _, c := range plan.Plan.Ordered {
		if c.Name == "core" {
			depOn = c.DependsOn
		}
	}
	if len(depOn) != 1 || depOn[0].Name != qualName(rootDataModule{}) || depOn[0].VersionConstraint != ">=0.1.0 <9.0.0" {
		t.Fatalf("constraint not carried: %+v", depOn)
	}
	// mutating the plan must not affect a new load
	core.DependsOn[0].Name = "corrupted"
	plan2, err := LoadRoot(&rootNamedMetaModule{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range plan2.Plan.Ordered {
		if c.Name == "core" && c.DependsOn[0].Name != qualName(rootDataModule{}) {
			t.Fatalf("plan is not detached: %+v", c.DependsOn)
		}
	}
}

func TestRootLoaderPinUnconstrained(t *testing.T) {
	plan, err := LoadRoot(&rootDataModule{}, &namedDataConsumer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range plan.Plan.Ordered {
		if m.Name == "consumer" {
			if len(m.DependsOn) != 1 || m.DependsOn[0].VersionConstraint != "=0.1.0" {
				t.Fatalf("unconstrained edge not pinned: %+v", m.DependsOn)
			}
		}
	}
}

type namedDataConsumer struct{}

func (m *namedDataConsumer) ModuleMetadata() Metadata { return Metadata{Name: "consumer"} }

func (m *namedDataConsumer) DependsOn() []Ref { return []Ref{Of[rootDataModule]()} }

func TestRootLoaderInvalidGraphs(t *testing.T) {
	for _, tc := range []struct {
		name string
		load func() (RootPlan, error)
		want string
	}{
		{"empty", func() (RootPlan, error) { return LoadRoot() }, "at least one root"},
		{"nil-root", func() (RootPlan, error) { return LoadRoot(nil) }, "nil root"},
		{"typed-nil", func() (RootPlan, error) {
			var p *rootDataModule
			return LoadRoot(p)
		}, "typed nil"},
		{"non-pointer", func() (RootPlan, error) { return LoadRoot(rootDataModule{}) }, "pointer"},
		{"unnamed-root", func() (RootPlan, error) {
			anon := struct{}{}
			return LoadRoot(&anon)
		}, "unnamed"},
		{"pointer-ref", func() (RootPlan, error) { return LoadRoot(&rootBadRefPointerModule{}) }, "*module.rootDataModule"},
		{"unnamed-ref", func() (RootPlan, error) { return LoadRoot(&rootBadRefUnnamedModule{}) }, "unnamed"},
		{"nil-ref", func() (RootPlan, error) { return LoadRoot(&rootBadRefNilModule{}) }, "nil"},
		{"meta-panic", func() (RootPlan, error) { return LoadRoot(&rootPanickyMetaModule{}) }, "panic in ModuleMetadata"},
		{"deps-panic", func() (RootPlan, error) { return LoadRoot(&rootPanickyDepsModule{}) }, "panic in DependsOn"},
		{"self-cycle", func() (RootPlan, error) { return LoadRoot(&rootSelfCycleModule{}) }, "module cycle"},
		{"cycle", func() (RootPlan, error) { return LoadRoot(&rootCycleAModule{}) }, "module cycle"},
		{"name-collision", func() (RootPlan, error) { return LoadRoot(&rootCollideOneModule{}, &rootCollideTwoModule{}) }, "collision"},
		{"invalid-version", func() (RootPlan, error) { return LoadRoot(&rootInvalidVersionModule{}) }, "invalid SemVer"},
		{"invalid-constraint", func() (RootPlan, error) { return LoadRoot(&rootBadRefInvalidConstraintModule{}) }, "invalid version constraint"},
		{"constraint-mismatch", func() (RootPlan, error) { return LoadRoot(&rootSharedConsumerOneModule{}) }, "does not satisfy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q: %v", tc.want, err)
			}
		})
	}
}

func TestRootLoaderRootDedup(t *testing.T) {
	root := &rootAuthModule{}
	plan, err := LoadRoot(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Ordered) != 2 || len(plan.Instances) != 2 {
		t.Fatalf("same root pointer not deduplicated: %+v", plan)
	}
}

type repeatedSharedRoot struct{}

func (m *repeatedSharedRoot) DependsOn() []Ref {
	return []Ref{Of[diamondSharedModule](), Of[diamondSharedModule]().Requires("=9.9.9")}
}

func TestRootLoaderRepeatedSharedEdgesValidated(t *testing.T) {
	_, err := LoadRoot(&repeatedSharedRoot{})
	if err == nil || !strings.Contains(err.Error(), "does not satisfy") {
		t.Fatalf("second shared edge constraint not validated: %v", err)
	}
}

func TestRootLoaderDistinctInstancesRejected(t *testing.T) {
	_, err := LoadRoot(&rootDataModule{}, &rootDataModule{})
	if err == nil || !strings.Contains(err.Error(), "distinct instances") {
		t.Fatalf("want distinct instance error: %v", err)
	}
}

func qualName(v any) string {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return derivedModuleName(t, "")
}

func TestRootLoaderTypeRefSemantics(t *testing.T) {
	ref := Of[rootAuthModule]()
	if ref.Type() != reflect.TypeOf(rootAuthModule{}) {
		t.Fatalf("ref type mismatch: %v", ref.Type())
	}
	if ref.VersionConstraint() != "" {
		t.Fatalf("default ref must be unconstrained, got %q", ref.VersionConstraint())
	}
	constrained := Of[rootAuthModule]().Requires("=0.1.0")
	if constrained.VersionConstraint() != "=0.1.0" {
		t.Fatalf("constraint not retained: %q", constrained.VersionConstraint())
	}
}

type nilTypedRefModule struct{}

func (m *nilTypedRefModule) DependsOn() []Ref {
	var typed *Type[rootDataModule]
	return []Ref{typed}
}

type nilCustomRefModule struct{}

func (m *nilCustomRefModule) DependsOn() []Ref {
	var typed *customRef
	return []Ref{typed}
}

type customRef struct{}

func (customRef) Type() reflect.Type        { return reflect.TypeOf(rootDataModule{}) }
func (customRef) VersionConstraint() string { return "=0.1.0" }

type panickingRefType struct{}

func (panickingRefType) Type() reflect.Type        { panic("type boom") }
func (panickingRefType) VersionConstraint() string { return "" }

type panickingRefConstraint struct{}

func (panickingRefConstraint) Type() reflect.Type        { return reflect.TypeOf(rootDataModule{}) }
func (panickingRefConstraint) VersionConstraint() string { panic("constraint boom") }

type refPanicAfterValidModule struct{}

func (m *refPanicAfterValidModule) DependsOn() []Ref {
	return []Ref{Of[rootDataModule](), panickingRefConstraint{}}
}

func TestRootLoaderRefSafety(t *testing.T) {
	_, err := LoadRoot(&nilTypedRefModule{})
	if err == nil || !strings.Contains(err.Error(), "typed nil Ref") {
		t.Fatalf("typed nil generic ref: %v", err)
	}
	_, err = LoadRoot(&nilCustomRefModule{})
	if err == nil || !strings.Contains(err.Error(), "typed nil Ref") {
		t.Fatalf("typed nil custom ref: %v", err)
	}
	plan, err := LoadRoot(&panickingRefTypeModule{})
	if err == nil || !strings.Contains(err.Error(), "panic in Ref.Type") {
		t.Fatalf("ref type panic: %v", err)
	}
	if plan.Instances != nil {
		t.Fatalf("instances survived a failed load: %v", plan.Instances)
	}
	plan, err = LoadRoot(&refPanicAfterValidModule{})
	if err == nil || !strings.Contains(err.Error(), "panic in Ref.VersionConstraint") {
		t.Fatalf("ref constraint panic: %v", err)
	}
	if len(plan.Instances) != 0 {
		t.Fatalf("no resource may be constructed on a failed load: %v", plan.Instances)
	}
}

type panickingRefTypeModule struct{}

func (m *panickingRefTypeModule) DependsOn() []Ref { return []Ref{panickingRefType{}} }

type genericLeaf[T any] struct{}

type genericConsumer struct{}

func (m *genericConsumer) DependsOn() []Ref {
	return []Ref{Of[genericLeaf[int]](), Of[genericLeaf[string]]()}
}

func TestRootLoaderGenericInstantiationNames(t *testing.T) {
	plan, err := LoadRoot(&genericConsumer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Ordered) != 3 || len(plan.Instances) != 3 {
		t.Fatalf("generic instantiations not planned: %d ordered, %d instances", len(plan.Plan.Ordered), len(plan.Instances))
	}
	intName := qualName(genericLeaf[int]{})
	stringName := qualName(genericLeaf[string]{})
	if intName == stringName {
		t.Fatalf("instantiations share a derived name: %s", intName)
	}
	if !strings.Contains(intName, "genericLeaf[int]") {
		t.Fatalf("instantiation identity missing: %s", intName)
	}
	qual := reflect.TypeOf(genericLeaf[int]{}).PkgPath()
	if !strings.HasPrefix(intName, qual) {
		t.Fatalf("derived name must use the full package path: %s", intName)
	}
	if plan.Instances[intName] == nil || plan.Instances[stringName] == nil {
		t.Fatalf("instantiation instances missing: %v", plan.Instances)
	}
}

type baseEmbedModule struct {
	Base
}

func (m *baseEmbedModule) DependsOn() []Ref { return []Ref{Of[rootDataModule]()} }

type bareBaseModule struct {
	Base
}

func TestRootLoaderBaseMetadata(t *testing.T) {
	m := &baseEmbedModule{Base: Base{Name: "auth", Version: "0.2.0", RequiredScopeKinds: []string{"org.organization"}}}
	plan, err := LoadRoot(m)
	if err != nil {
		t.Fatal(err)
	}
	auth := plan.Plan.Ordered[1]
	if auth.Name != "auth" || auth.Version != "0.2.0" || !reflect.DeepEqual(auth.RequiredScopeKinds, []string{"org.organization"}) {
		t.Fatalf("base metadata not applied: %+v", auth)
	}
	if plan.Plan.Ordered[0].Name != qualName(rootDataModule{}) {
		t.Fatalf("derived dep name: %q", plan.Plan.Ordered[0].Name)
	}
	copy1 := m.ModuleMetadata()
	copy1.RequiredScopeKinds = append(copy1.RequiredScopeKinds, "mutated")
	if kept := m.ModuleMetadata().RequiredScopeKinds; len(kept) != 1 || kept[0] != "org.organization" {
		t.Fatalf("ModuleMetadata copy is not defensive: %v", kept)
	}
	barePlan, err := LoadRoot(&bareBaseModule{})
	if err != nil {
		t.Fatal(err)
	}
	bare := barePlan.Plan.Ordered[0]
	if bare.Name != qualName(bareBaseModule{}) || bare.Version != "0.1.0" || len(bare.RequiredScopeKinds) != 0 {
		t.Fatalf("bare base defaults: %+v", bare)
	}
}
