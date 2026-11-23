package module

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type registry struct {
	mu      sync.Mutex
	modules map[string]Module
}

// NewRegistry creates an explicit, compile-time module registry.
func NewRegistry() Registry { return &registry{modules: make(map[string]Module)} }

func clone(m Module) Module {
	m.DependsOn = append([]Dependency(nil), m.DependsOn...)
	m.RequiredScopeKinds = append([]string(nil), m.RequiredScopeKinds...)
	return m
}

func (r *registry) Register(m Module) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("module name is required")
	}
	if _, exists := r.modules[m.Name]; exists {
		return fmt.Errorf("duplicate module %s", m.Name)
	}
	r.modules[m.Name] = clone(m)
	return nil
}

func (r *registry) Plan() (Plan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	plan := Plan{}
	names := make([]string, 0, len(r.modules))
	for name := range r.modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := r.modules[name]
		if _, err := parseVersion(m.Version); err != nil {
			return plan, fmt.Errorf("module %s: %w", name, err)
		}
		seen := map[string]bool{}
		for _, dep := range m.DependsOn {
			d, exists := r.modules[dep.Name]
			if !exists {
				return plan, fmt.Errorf("dependency path %s -> %s: missing module", name, dep.Name)
			}
			if seen[dep.Name] {
				return plan, fmt.Errorf("dependency path %s -> %s: duplicate dependency", name, dep.Name)
			}
			seen[dep.Name] = true
			ok, err := matches(d.Version, dep.VersionConstraint)
			if err != nil || !ok {
				return plan, fmt.Errorf("dependency path %s -> %s: version %s does not satisfy %q (%v)", name, dep.Name, d.Version, dep.VersionConstraint, err)
			}
		}
	}
	done := map[string]bool{}
	for len(done) < len(names) {
		layer := []string{}
		for _, name := range names {
			if done[name] {
				continue
			}
			ready := true
			for _, dep := range r.modules[name].DependsOn {
				ready = ready && done[dep.Name]
			}
			if ready {
				layer = append(layer, name)
			}
		}
		if len(layer) == 0 {
			// Follow unresolved edges until a node repeats, retaining the actual cycle.
			path, seen, next := []string{}, map[string]int{}, ""
			for _, name := range names {
				if !done[name] {
					next = name
					break
				}
			}
			for {
				if i, exists := seen[next]; exists {
					return Plan{}, fmt.Errorf("module cycle: %s", strings.Join(append(path[i:], next), " -> "))
				}
				seen[next] = len(path)
				path = append(path, next)
				for _, dep := range r.modules[next].DependsOn {
					if !done[dep.Name] {
						next = dep.Name
						break
					}
				}
			}
		}
		plan.Layers = append(plan.Layers, layer)
		for _, name := range layer {
			done[name] = true
			plan.Ordered = append(plan.Ordered, clone(r.modules[name]))
		}
	}
	return plan, nil
}

type version struct {
	core [3]string
	pre  []string
}

func parseVersion(s string) (version, error) {
	v := version{}
	invalid := func() (version, error) { return v, fmt.Errorf("invalid SemVer %q", s) }
	base, build, hasBuild := strings.Cut(s, "+")
	validIDs := func(ids string, numericStrict bool) bool {
		for _, id := range strings.Split(ids, ".") {
			if id == "" {
				return false
			}
			numeric := true
			for _, c := range id {
				if !(c >= '0' && c <= '9') {
					numeric = false
				}
				if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
					return false
				}
			}
			if numericStrict && numeric && len(id) > 1 && id[0] == '0' {
				return false
			}
		}
		return true
	}
	if hasBuild && !validIDs(build, false) {
		return invalid()
	}
	core, pre, hasPre := strings.Cut(base, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return invalid()
	}
	for i, p := range parts {
		if p == "" || len(p) > 1 && p[0] == '0' {
			return invalid()
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return invalid()
			}
		}
		v.core[i] = p
	}
	if hasPre {
		if !validIDs(pre, true) {
			return invalid()
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, nil
}

func compare(a, b version) int {
	for i := range a.core {
		if len(a.core[i]) < len(b.core[i]) {
			return -1
		}
		if len(a.core[i]) > len(b.core[i]) {
			return 1
		}
		if a.core[i] < b.core[i] {
			return -1
		}
		if a.core[i] > b.core[i] {
			return 1
		}
	}
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		if x == y {
			continue
		}
		numeric := func(s string) bool {
			for _, c := range s {
				if c < '0' || c > '9' {
					return false
				}
			}
			return true
		}
		xn, yn := numeric(x), numeric(y)
		if xn != yn {
			if xn {
				return -1
			}
			return 1
		}
		if xn && len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
		if x < y {
			return -1
		}
		return 1
	}
	if len(a.pre) < len(b.pre) {
		return -1
	}
	if len(a.pre) > len(b.pre) {
		return 1
	}
	return 0
}

func matches(s, constraint string) (bool, error) {
	v, err := parseVersion(s)
	if err != nil {
		return false, err
	}
	if strings.HasPrefix(constraint, "=") {
		want, err := parseVersion(strings.TrimPrefix(constraint, "="))
		return err == nil && compare(v, want) == 0, err
	}
	parts := strings.Fields(constraint)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], ">=") || !strings.HasPrefix(parts[1], "<") {
		return false, fmt.Errorf("unsupported version constraint")
	}
	lo, err := parseVersion(strings.TrimPrefix(parts[0], ">="))
	if err != nil {
		return false, err
	}
	hi, err := parseVersion(strings.TrimPrefix(parts[1], "<"))
	if err != nil {
		return false, err
	}
	if len(lo.pre) > 0 || len(hi.pre) > 0 {
		return false, fmt.Errorf("prerelease requires exact constraint")
	}
	return len(v.pre) == 0 && compare(v, lo) >= 0 && compare(v, hi) < 0, nil
}
