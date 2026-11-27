package module

import (
	"fmt"
	"reflect"
	"strings"
)

// defaultModuleVersion is the base metadata version used when a module does
// not declare a Version (ADR11: the product version stays 0.1.0).
const defaultModuleVersion = "0.1.0"

// RootPlan pairs the legacy module metadata snapshot with the actual module
// instances resolved from a root Type closure.
type RootPlan struct {
	// Plan stores the legacy Module metadata in stable topological order
	// (existing Registry.Plan); lifecycle hooks are nil because hook wiring
	// is owned by the root runtime.
	Plan Plan
	// Instances maps stable module name to actual instance: provided root
	// pointers are retained, Type dependencies are typed zero instances
	// created once per Type.
	Instances map[string]any
}

// LoadRoot builds the module graph from the transitive Type closure of the
// given root modules, deduplicating shared dependencies by Type. All graph
// validation (typed nils, invalid refs, unnamed struct types, cycles, name
// collisions, invalid versions and unsatisfied constraints) runs before any
// lifecycle hook or resource constructor: the loader executes no hooks and
// creates no resources beyond reflect.New zero instances.
func LoadRoot(roots ...any) (RootPlan, error) {
	if len(roots) == 0 {
		return RootPlan{}, fmt.Errorf("root loading requires at least one root module")
	}
	graph := &rootGraph{byType: map[reflect.Type]*rootNode{}}
	for i, root := range roots {
		if root == nil {
			return RootPlan{}, fmt.Errorf("root %d: nil root module", i)
		}
		rootType := reflect.TypeOf(root)
		if rootType.Kind() != reflect.Pointer {
			return RootPlan{}, fmt.Errorf("root %d: %s must be a pointer to a named struct module", i, rootType)
		}
		if reflect.ValueOf(root).IsNil() {
			return RootPlan{}, fmt.Errorf("root %d: typed nil pointer of type %s", i, rootType)
		}
		instanceType := rootType.Elem()
		if err := validateModuleType(instanceType); err != nil {
			return RootPlan{}, fmt.Errorf("root %d: %w", i, err)
		}
		if existing, ok := graph.byType[instanceType]; ok {
			if existing.instance == root {
				continue // the same instance was provided twice; the Type is planned once
			}
			return RootPlan{}, fmt.Errorf("root %d: two distinct instances provided for module type %s", i, instanceType)
		}
		declared, err := resolveMetadata(root)
		if err != nil {
			return RootPlan{}, fmt.Errorf("root %d: %w", i, err)
		}
		graph.add(&rootNode{typ: instanceType, instance: root, meta: defaultMetadata(instanceType, declared)})
	}
	if err := collectDependencies(graph); err != nil {
		return RootPlan{}, err
	}
	if err := detectCycles(graph); err != nil {
		return RootPlan{}, err
	}
	if err := validateRootMetadata(graph); err != nil {
		return RootPlan{}, err
	}
	plan, err := registerRootModules(graph)
	if err != nil {
		return RootPlan{}, err
	}
	instances := make(map[string]any, len(graph.nodes))
	for _, node := range graph.nodes {
		instances[node.meta.Name] = node.instance
	}
	return RootPlan{Plan: plan, Instances: instances}, nil
}

type rootNode struct {
	typ      reflect.Type
	instance any
	meta     Metadata
	deps     []rootEdge
}

type rootEdge struct {
	refType    reflect.Type
	constraint string
}

type rootGraph struct {
	nodes  []*rootNode // insertion order keeps validation and messages deterministic
	byType map[reflect.Type]*rootNode
}

func (g *rootGraph) add(node *rootNode) {
	g.nodes = append(g.nodes, node)
	g.byType[node.typ] = node
}

func validateModuleType(t reflect.Type) error {
	if t == nil {
		return fmt.Errorf("module type reference is invalid: nil type")
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("module type reference %s is invalid: only named struct types can be modules, got %s", t, t.Kind())
	}
	if t.Name() == "" {
		return fmt.Errorf("module type reference %s is invalid: unnamed struct types cannot be modules", t)
	}
	return nil
}

func resolveMetadata(instance any) (meta Metadata, err error) {
	provider, ok := instance.(MetadataProvider)
	if !ok {
		return Metadata{}, nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in ModuleMetadata: %v", r)
			meta = Metadata{}
		}
	}()
	return provider.ModuleMetadata(), nil
}

func defaultMetadata(instanceType reflect.Type, declared Metadata) Metadata {
	meta := Metadata{
		Name:               derivedModuleName(instanceType, declared.Name),
		Version:            defaultModuleVersion,
		RequiredScopeKinds: append([]string(nil), declared.RequiredScopeKinds...),
	}
	if version := strings.TrimSpace(declared.Version); version != "" {
		meta.Version = version
	}
	return meta
}

func derivedModuleName(instanceType reflect.Type, declared string) string {
	if name := strings.TrimSpace(declared); name != "" {
		return name
	}
	if pkg := instanceType.PkgPath(); pkg != "" && instanceType.Name() != "" {
		qualified := pkg + "." + instanceType.Name()
		// reflect renders instantiated generics with their type arguments
		// (pkg.Generic[int]); keep that identity so distinct instantiations
		// derive distinct, collision-safe names.
		if display := instanceType.String(); len(display) > len(qualified) {
			qualified += display[len(qualified):]
		}
		return qualified
	}
	// Unnamed or synthetic types keep reflect's rendering; the name-collision
	// check rejects any resulting duplicate deterministically.
	return instanceType.String()
}

func collectDependencies(graph *rootGraph) error {
	for i := 0; i < len(graph.nodes); i++ {
		node := graph.nodes[i]
		refs, err := declaredRefs(node.instance)
		if err != nil {
			return fmt.Errorf("module %s: %w", node.typ, err)
		}
		for j, ref := range refs {
			if ref == nil {
				return fmt.Errorf("module %s: DependsOn()[%d] is nil", node.typ, j)
			}
			if isNilRef(ref) {
				return fmt.Errorf("module %s: DependsOn()[%d] is a typed nil Ref of type %s", node.typ, j, reflect.TypeOf(ref))
			}
			refType, err := safeRefType(node.typ, j, ref)
			if err != nil {
				return err
			}
			if err := validateModuleType(refType); err != nil {
				return fmt.Errorf("module %s: DependsOn()[%d]: %w", node.typ, j, err)
			}
			constraint, err := safeRefConstraint(node.typ, j, ref)
			if err != nil {
				return err
			}
			node.deps = append(node.deps, rootEdge{refType: refType, constraint: constraint})
			if _, exists := graph.byType[refType]; !exists {
				// Type dependencies receive a typed zero instance, exactly once per Type.
				instance := reflect.New(refType).Interface()
				declared, err := resolveMetadata(instance)
				if err != nil {
					return fmt.Errorf("module %s: %w", refType, err)
				}
				graph.add(&rootNode{typ: refType, instance: instance, meta: defaultMetadata(refType, declared)})
			}
		}
	}
	return nil
}

func isNilRef(ref Ref) bool {
	value := reflect.ValueOf(ref)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

// safeRefType invokes ref.Type(), converting typed nil receivers and method
// panics into contextual load errors instead of escaping the loader.
func safeRefType(nodeType reflect.Type, index int, ref Ref) (t reflect.Type, err error) {
	defer func() {
		if r := recover(); r != nil {
			t, err = nil, fmt.Errorf("module %s: DependsOn()[%d]: panic in Ref.Type: %v", nodeType, index, r)
		}
	}()
	return ref.Type(), nil
}

// safeRefConstraint invokes ref.VersionConstraint() under the same recovery.
func safeRefConstraint(nodeType reflect.Type, index int, ref Ref) (c string, err error) {
	defer func() {
		if r := recover(); r != nil {
			c, err = "", fmt.Errorf("module %s: DependsOn()[%d]: panic in Ref.VersionConstraint: %v", nodeType, index, r)
		}
	}()
	return ref.VersionConstraint(), nil
}

func declaredRefs(instance any) (refs []Ref, err error) {
	depender, ok := instance.(Depender)
	if !ok {
		return nil, nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in DependsOn: %v", r)
			refs = nil
		}
	}()
	return depender.DependsOn(), nil
}

func detectCycles(graph *rootGraph) error {
	const unvisited, visiting, done = 0, 1, 2
	state := make(map[reflect.Type]int, len(graph.nodes))
	var path []reflect.Type
	var visit func(reflect.Type) error
	visit = func(t reflect.Type) error {
		switch state[t] {
		case done:
			return nil
		case visiting:
			start := 0
			for i, p := range path {
				if p == t {
					start = i
					break
				}
			}
			names := make([]string, 0, len(path)-start+1)
			for _, p := range path[start:] {
				names = append(names, p.String())
			}
			names = append(names, t.String())
			return fmt.Errorf("module cycle: %s", strings.Join(names, " -> "))
		}
		state[t] = visiting
		path = append(path, t)
		for _, edge := range graph.byType[t].deps {
			if err := visit(edge.refType); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[t] = done
		return nil
	}
	for _, node := range graph.nodes {
		if err := visit(node.typ); err != nil {
			return err
		}
	}
	return nil
}

func validateRootMetadata(graph *rootGraph) error {
	owners := make(map[string]reflect.Type, len(graph.nodes))
	for _, node := range graph.nodes {
		if _, err := parseVersion(node.meta.Version); err != nil {
			return fmt.Errorf("module %s: %w", node.typ, err)
		}
		if prev, dup := owners[node.meta.Name]; dup {
			return fmt.Errorf("module name collision: %q used by both %s and %s; declare distinct Metadata.Name values", node.meta.Name, prev, node.typ)
		}
		owners[node.meta.Name] = node.typ
	}
	for _, node := range graph.nodes {
		for _, edge := range node.deps {
			if strings.TrimSpace(edge.constraint) == "" {
				continue // unconstrained: accepted and pinned to the resolved version below
			}
			target := graph.byType[edge.refType].meta.Version
			ok, err := matches(target, edge.constraint)
			if err != nil {
				return fmt.Errorf("module %s -> %s: invalid version constraint %q: %v", node.typ, edge.refType, edge.constraint, err)
			}
			if !ok {
				return fmt.Errorf("module %s -> %s: version %s does not satisfy %q", node.typ, edge.refType, target, edge.constraint)
			}
		}
	}
	return nil
}

type translatedDependency struct {
	explicit string
	pinned   string
}

func registerRootModules(graph *rootGraph) (Plan, error) {
	registry := NewRegistry()
	for _, node := range graph.nodes {
		translated := map[reflect.Type]*translatedDependency{}
		var order []reflect.Type
		for _, edge := range node.deps {
			dep, exists := translated[edge.refType]
			if !exists {
				dep = &translatedDependency{pinned: "=" + graph.byType[edge.refType].meta.Version}
				translated[edge.refType] = dep
				order = append(order, edge.refType)
			}
			if strings.TrimSpace(edge.constraint) != "" && dep.explicit == "" {
				dep.explicit = edge.constraint
			}
		}
		dependsOn := make([]Dependency, 0, len(order))
		for _, depType := range order {
			constraint := translated[depType].pinned
			if explicit := translated[depType].explicit; explicit != "" {
				constraint = explicit
			}
			dependsOn = append(dependsOn, Dependency{
				Name:              graph.byType[depType].meta.Name,
				VersionConstraint: constraint,
			})
		}
		m := Module{
			Name:               node.meta.Name,
			Version:            node.meta.Version,
			DependsOn:          dependsOn,
			RequiredScopeKinds: append([]string(nil), node.meta.RequiredScopeKinds...),
		}
		if err := registry.Register(m); err != nil {
			return Plan{}, err
		}
	}
	return registry.Plan()
}
