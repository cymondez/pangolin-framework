package config

import (
	"strings"
	"testing"
)

func TestA47(t *testing.T) {
	t.Run("precedence", func(t *testing.T) {
		layers := []Source{{"cli", map[string]string{"key": "cli"}}, {"environment", map[string]string{"key": "environment"}}, {"file", map[string]string{"key": "file"}}, {"defaults", map[string]string{"key": "defaults"}}}
		for i, want := range []string{"cli", "environment", "file", "defaults"} {
			r, e := NewLoader(nil).Load(layers[i:])
			if e != nil {
				t.Fatal(e)
			}
			if v, _ := r.Lookup("key"); v != want {
				t.Fatal(v)
			}
		}
	})
	t.Run("immutable", func(t *testing.T) {
		values := map[string]string{"key": "old"}
		r, _ := NewLoader(nil).Load([]Source{{"file", values}})
		values["key"] = "new"
		if v, _ := r.Lookup("key"); v != "old" {
			t.Fatal(v)
		}
		if _, ok := r.Lookup("missing"); ok {
			t.Fatal("missing")
		}
	})
	t.Run("missing-timeouts", func(t *testing.T) {
		if _, e := NewLoader(StartupRules()).Load(nil); e == nil {
			t.Fatal("missing settings")
		}
	})
	t.Run("redacted", func(t *testing.T) {
		_, e := NewLoader([]Rule{{"dsn", true, URL("postgres")}}).Load([]Source{{"environment", map[string]string{"dsn": "secret-password"}}})
		if e == nil || strings.Contains(e.Error(), "secret-password") {
			t.Fatal(e)
		}
	})
}

func TestLoadValidation(t *testing.T) {
	l := NewLoader(StartupRules())
	v := map[string]string{"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s", "uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4"}
	if _, e := l.Load([]Source{{"defaults", v}}); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"uow.default_timeout", "uow.max_requires_new_depth"} {
		old := v[key]
		for _, bad := range []string{"0", "-1", "garbage"} {
			v[key] = bad
			if _, e := l.Load([]Source{{"file", v}}); e == nil {
				t.Fatal(key, bad)
			}
		}
		v[key] = old
	}
	for _, sources := range [][]Source{{{"bad", nil}}, {{"file", nil}, {"file", nil}}, {{"file", map[string]string{"": "x"}}}} {
		if _, e := l.Load(sources); e == nil {
			t.Fatal(sources)
		}
	}
	if e := URL("postgres", "postgresql")("postgresql://u:p@localhost/db"); e != nil {
		t.Fatal(e)
	}
	if e := URL("postgres")("http://localhost"); e == nil {
		t.Fatal("scheme")
	}
}
