package features

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/pangolin-framework/pangolin/platform/settings"
	"github.com/pangolin-framework/pangolin/security/scope"
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

type recordingSource struct {
	values []json.RawMessage
	seen   []scope.ScopeBinding
	err    error
}

func (s *recordingSource) Resolve(ctx context.Context, _ string) (settings.Result, error) {
	if b, ok := scope.Current(ctx); ok {
		s.seen = append(s.seen, b)
	}
	r := settings.Result{Candidates: s.values}
	if len(s.values) > 0 {
		r.Value = s.values[0]
	}
	return r, s.err
}

// Features are constrained by Permission, never the reverse: Evaluate first
// passes the current Permission, then evaluates availability. Feature values
// never grant permissions and disabled features never change Permission
// results; ordinary Permission stays independent of Feature state.
func TestFeaturePermissionBeforeAvailability(t *testing.T) {
	t.Run("permission-allow-feature-off", func(t *testing.T) {
		calls, authCalls := 0, 0
		s, _ := New(source{values: []json.RawMessage{json.RawMessage(`false`)}, calls: &calls}, authority{calls: &authCalls}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Boolean}})
		v, err := s.Evaluate(bound(t), "f")
		if err != nil || v.Boolean {
			t.Fatal("disabled feature must not flip permission or error", v, err)
		}
		if authCalls != 1 || calls != 1 {
			t.Fatal("permission first, then availability", authCalls, calls)
		}
	})
	t.Run("permission-deny-feature-on", func(t *testing.T) {
		calls, authCalls := 0, 0
		s, _ := New(source{values: []json.RawMessage{json.RawMessage(`true`)}, calls: &calls}, authority{err: scope.ErrDenied, calls: &authCalls}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Boolean}})
		if _, err := s.Evaluate(bound(t), "f"); !errors.Is(err, scope.ErrDenied) {
			t.Fatal("feature on never grants permission", err)
		}
		if authCalls != 1 || calls != 0 {
			t.Fatal("denied permission must deny without reading settings", authCalls, calls)
		}
	})
	t.Run("both-allow", func(t *testing.T) {
		s, _ := New(source{values: []json.RawMessage{json.RawMessage(`true`)}}, authority{}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Boolean}})
		v, err := s.Evaluate(bound(t), "f")
		if err != nil || !v.Boolean {
			t.Fatal(v, err)
		}
	})
	t.Run("authorization-source-failure-preserves-cause", func(t *testing.T) {
		offline := errors.New("authority offline")
		calls := 0
		s, _ := New(source{values: []json.RawMessage{json.RawMessage(`true`)}, calls: &calls}, authority{err: offline}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Boolean}})
		_, err := s.Evaluate(bound(t), "f")
		if !errors.Is(err, offline) {
			t.Fatal("authorization cause must be preserved", err)
		}
		if calls != 0 {
			t.Fatal("availability must not be evaluated during an authorization outage", calls)
		}
	})
	t.Run("no-binding-denies-without-evaluation", func(t *testing.T) {
		calls, authCalls := 0, 0
		s, _ := New(source{values: []json.RawMessage{json.RawMessage(`true`)}, calls: &calls}, authority{calls: &authCalls}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Boolean}})
		if _, err := s.Evaluate(context.Background(), "f"); !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
		if authCalls != 0 || calls != 0 {
			t.Fatal("unbound context must not evaluate permission or availability", authCalls, calls)
		}
	})
	t.Run("scope-kind-denied-before-settings", func(t *testing.T) {
		calls := 0
		s, _ := New(source{values: []json.RawMessage{json.RawMessage(`true`)}, calls: &calls}, authority{}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Boolean, ScopeKinds: []string{"org.organization"}}})
		if _, err := s.Evaluate(bound(t), "f"); !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Fatal("out-of-range scope must not read settings", calls)
		}
	})
	t.Run("settings-resolve-under-current-owner", func(t *testing.T) {
		src := &recordingSource{values: []json.RawMessage{json.RawMessage(`true`)}}
		s, _ := New(src, authority{}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Boolean}})
		ctx := bound(t)
		b, _ := scope.Current(ctx)
		v, err := s.Evaluate(ctx, "f")
		if err != nil || !v.Boolean {
			t.Fatal(v, err)
		}
		if len(src.seen) != 1 || src.seen[0].Ref != b.Ref || src.seen[0].Principal != b.Principal {
			t.Fatal("feature must resolve settings under its own binding", src.seen)
		}
	})
	t.Run("quota-ceiling-still-applies-after-permission", func(t *testing.T) {
		s, _ := New(source{values: []json.RawMessage{json.RawMessage(`100`), json.RawMessage(`25`)}}, authority{}, map[string]Definition{"f": {Setting: "f", Permission: "use", Kind: Quota, Ceiling: true}})
		v, err := s.Evaluate(bound(t), "f")
		if err != nil || v.Quota != 25 {
			t.Fatal("ceiling must still intersect candidates after permission passes", v, err)
		}
	})
}
