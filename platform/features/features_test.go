package features

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cymondez/pangolin-framework/platform/settings"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type authority struct {
	err   error
	calls *int
}

func (a authority) Authorize(context.Context, string) error {
	if a.calls != nil {
		*a.calls++
	}
	return a.err
}

type source struct {
	values []json.RawMessage
	calls  *int
	err    error
}

func (s source) Resolve(context.Context, string) (settings.Result, error) {
	if s.calls != nil {
		*s.calls++
	}
	r := settings.Result{Candidates: s.values}
	if len(s.values) > 0 {
		r.Value = s.values[0]
	}
	return r, s.err
}

type fixture struct{}

func (fixture) Kinds() []string { return []string{"tenant"} }
func (fixture) Resolve(_ context.Context, p scope.PrincipalRef, r scope.ScopeRef) (scope.ScopeBinding, error) {
	return scope.ScopeBinding{Ref: r, Principal: p, AuthorityRevision: "1"}, nil
}
func (fixture) Authorize(context.Context, scope.ScopeBinding, string) error { return nil }
func bound(t *testing.T) context.Context {
	r, _ := scope.NewRegistry(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "u", Kind: "user"}, "t", true
	}, fixture{})
	c, e := r.Bind(context.Background(), scope.ScopeRef{TenantID: "t", Kind: "tenant", ID: "t"})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestA65Features(t *testing.T) {
	t.Run("feature-permission", func(t *testing.T) {
		calls := 0
		s, _ := New(source{values: []json.RawMessage{json.RawMessage(`true`)}, calls: &calls}, authority{err: scope.ErrDenied}, map[string]Definition{"edit": {Setting: "edit", Permission: "books.write", Kind: Boolean}})
		if _, err := s.Evaluate(bound(t), "edit"); !errors.Is(err, scope.ErrDenied) || calls != 0 {
			t.Fatal("feature evaluated before permission", err, calls)
		}
	})
}
func TestA80Features(t *testing.T) {
	t.Run("parent-security-restriction", func(t *testing.T) {
		for _, tc := range []struct {
			kind    Kind
			values  []string
			quota   int64
			boolean bool
			enum    string
		}{{Boolean, []string{"true", "false", "true"}, 0, false, ""}, {Quota, []string{"100", "10", "50"}, 10, false, ""}, {Enum, []string{`"pro"`, `"basic"`}, 0, false, "basic"}} {
			raw := []json.RawMessage{}
			for _, v := range tc.values {
				raw = append(raw, json.RawMessage(v))
			}
			s, _ := New(source{values: raw}, authority{}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: tc.kind, Ceiling: true, Options: []string{"basic", "pro"}}})
			v, e := s.Evaluate(bound(t), "f")
			if e != nil || v.Quota != tc.quota || v.Boolean != tc.boolean || v.Enum != tc.enum {
				t.Fatal(v, e)
			}
		}
	})
}
func TestFeatureErrorsAndTypes(t *testing.T) {
	limited, _ := New(source{values: []json.RawMessage{json.RawMessage(`true`)}}, authority{}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Boolean, ScopeKinds: []string{"org.organization"}}})
	if _, err := limited.Evaluate(bound(t), "f"); !errors.Is(err, scope.ErrDenied) {
		t.Fatal("feature scope range ignored", err)
	}
	if _, e := New(nil, authority{}, nil); e == nil {
		t.Fatal("nil resolver")
	}
	if _, e := New(source{}, authority{}, map[string]Definition{"f": {}}); e == nil {
		t.Fatal("definition")
	}
	for _, tc := range []struct {
		kind  Kind
		value string
		valid bool
	}{{Boolean, "true", true}, {Boolean, "null", false}, {Boolean, " null ", false}, {Boolean, "1", false}, {Quota, "5", true}, {Quota, " null ", false}, {Quota, "-1", false}, {Quota, "1.1", false}, {Enum, `"basic"`, true}, {Enum, `"unknown"`, false}} {
		s, _ := New(source{values: []json.RawMessage{json.RawMessage(tc.value)}}, authority{}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: tc.kind, Options: []string{"basic", "pro"}}})
		_, err := s.Evaluate(bound(t), "f")
		if (err == nil) != tc.valid {
			t.Fatal(tc, err)
		}
		if _, err = s.Evaluate(bound(t), "unknown"); err == nil {
			t.Fatal("unknown")
		}
	}
	s, _ := New(source{err: errors.New("offline")}, authority{}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Boolean}})
	if _, err := s.Evaluate(bound(t), "f"); err == nil {
		t.Fatal("provider error")
	}
}
