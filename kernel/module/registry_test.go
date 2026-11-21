package module

import (
	"reflect"
	"strings"
	"testing"
)

func TestA01(t *testing.T) {
	for _, tc := range []struct {
		name    string
		modules []Module
		want    string
	}{
		{"duplicate-name", []Module{{Name: "a", Version: "0.1.0"}, {Name: "a", Version: "0.1.0"}}, "duplicate"},
		{"missing-dependency", []Module{{Name: "a", Version: "0.1.0", DependsOn: []Dependency{{"b", "=0.1.0"}}}}, "a -> b"},
		{"incompatible-version", []Module{{Name: "a", Version: "0.1.0"}, {Name: "b", Version: "0.1.0", DependsOn: []Dependency{{"a", "=0.2.0"}}}}, "b -> a"},
		{"cycle", []Module{{Name: "a", Version: "0.1.0", DependsOn: []Dependency{{"b", "=0.1.0"}}}, {Name: "b", Version: "0.1.0", DependsOn: []Dependency{{"a", "=0.1.0"}}}}, "a -> b -> a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry()
			var err error
			for _, m := range tc.modules {
				if err = r.Register(m); err != nil {
					break
				}
			}
			if err == nil {
				_, err = r.Plan()
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s: %v", tc.want, err)
			}
		})
	}
	t.Run("stable-layers", func(t *testing.T) {
		r := NewRegistry()
		for _, name := range []string{"c", "b", "a"} {
			m := Module{Name: name, Version: "0.1.0"}
			if name != "a" {
				m.DependsOn = []Dependency{{"a", ">=0.1.0 <0.2.0"}}
			}
			if err := r.Register(m); err != nil {
				t.Fatal(err)
			}
			if len(m.DependsOn) > 0 {
				m.DependsOn[0].Name = "corrupted"
			}
		}
		p, e := r.Plan()
		if e != nil || !reflect.DeepEqual(p.Layers, [][]string{{"a"}, {"b", "c"}}) {
			t.Fatalf("%+v %v", p, e)
		}
		p.Ordered[1].DependsOn[0].Name = "corrupted"
		p.Layers[0][0] = "corrupted"
		if _, e := r.Plan(); e != nil {
			t.Fatal(e)
		}
	})
}

func TestVersionGrammar(t *testing.T) {
	for _, s := range []string{"0.1.0", "1.2.3-alpha.1+build.01", "1.0.0-999999999999999999999999", "1.2.18446744073709551616"} {
		if _, e := parseVersion(s); e != nil {
			t.Fatal(s, e)
		}
	}
	for _, s := range []string{"", "1.2", "1.2.3.4", "01.2.3", "1.2.+3", "1.2.3-", "1.2.3-01", "1.2.3+a..b", "1.2.3-@"} {
		if _, e := parseVersion(s); e == nil {
			t.Fatal(s)
		}
	}
	for _, c := range []string{"*", "^0.1.0", "~0.1.0", "0.1", ">=0.1.0 || <0.2.0", ">=0.1.0-rc.1 <0.2.0", ">=x <0.2.0", ">=0.1.0 <x", "=x"} {
		if _, e := matches("0.1.0", c); e == nil {
			t.Fatal(c)
		}
	}
	for _, pair := range [][2]string{{"0.1.0+build", "=0.1.0"}, {"0.1.0-rc.1", "=0.1.0-rc.1"}} {
		ok, e := matches(pair[0], pair[1])
		if !ok || e != nil {
			t.Fatal(pair, e)
		}
	}
	versions := []string{"0.0.9", "0.1.0-alpha", "0.1.0-alpha.1", "0.1.0-alpha.2", "0.1.0-alpha.10", "0.1.0-beta", "0.1.0-rc.1", "0.1.0", "1.0.0"}
	for i, a := range versions {
		for j, b := range versions {
			va, _ := parseVersion(a)
			vb, _ := parseVersion(b)
			n := compare(va, vb)
			if i < j && n >= 0 || i > j && n <= 0 || i == j && n != 0 {
				t.Fatal(a, b, n)
			}
		}
	}
	r := NewRegistry()
	if r.Register(Module{}) == nil {
		t.Fatal("empty name")
	}
	_ = r.Register(Module{Name: "a", Version: "bad"})
	if _, e := r.Plan(); e == nil {
		t.Fatal("invalid version")
	}
	r = NewRegistry()
	_ = r.Register(Module{Name: "a", Version: "0.1.0"})
	_ = r.Register(Module{Name: "b", Version: "0.1.0", DependsOn: []Dependency{{"a", "=0.1.0"}, {"a", "=0.1.0"}}})
	if _, e := r.Plan(); e == nil {
		t.Fatal("duplicate dependency")
	}
}
