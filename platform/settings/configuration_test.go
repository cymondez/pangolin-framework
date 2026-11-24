package settings

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/platform/cache"
	"github.com/cymondez/pangolin-framework/security/scope"
)

func TestA65JSONConfiguration(t *testing.T) {
	ctx := contextFor(t, "tenant", "tenant")
	binding, _ := scope.Current(ctx)
	reader, err := config.ParseJSON([]byte(`{"Settings":{"Channels":[{"Name":"mail"},{"Name":"sms"}],"List":[0,1,2,3,4,5,6,7,8,9,10,11],"Grid":[["nested"]],"0":"property","Abp.Mailing.Smtp.Host":"literal","Abp":{"Mailing":{"Smtp":{"Host":"nested"}}},"Null":null,"EmptyObject":{},"EmptyArray":[],"TextNumber":"123","Bool":true,"Quota":25}}`))
	if err != nil {
		t.Fatal(err)
	}
	base := NewMemoryProvider(nil)
	source, err := WithConfiguration(base, reader, map[string]func(string) (json.RawMessage, error){"Quota": func(s string) (json.RawMessage, error) {
		n, e := strconv.ParseInt(s, 10, 64)
		if e != nil {
			return nil, e
		}
		b, e := json.Marshal(n)
		return b, e
	}})
	if err != nil {
		t.Fatal(err)
	}
	definitions := []Definition{}
	for _, key := range []string{"Channels:0:Name", "Channels:1:Name", "List:11", "Grid:0:0", "0", "Abp.Mailing.Smtp.Host", "Abp:Mailing:Smtp:Host", "Null", "EmptyObject", "EmptyArray", "TextNumber", "Bool", "Quota"} {
		definitions = append(definitions, Definition{Key: key, Default: json.RawMessage(`"fallback"`), Inherit: true})
	}
	memory, _ := cache.NewMemory("app", "v", 32)
	service, err := New(source, memory, nil, time.Hour, definitions...)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("json-flatmap", func(t *testing.T) {
		for key, want := range map[string]string{"Channels:0:Name": `"mail"`, "Channels:1:Name": `"sms"`, "List:11": `"11"`, "Grid:0:0": `"nested"`, "0": `"property"`, "Abp.Mailing.Smtp.Host": `"literal"`, "Abp:Mailing:Smtp:Host": `"nested"`, "TextNumber": `"123"`, "Bool": `"True"`, "Quota": `25`} {
			got, e := service.Resolve(ctx, key)
			if e != nil || string(got.Value) != want {
				t.Fatal(key, got, e)
			}
		}
	})
	t.Run("configuration-fallback", func(t *testing.T) {
		for _, key := range []string{"Null", "EmptyObject"} {
			got, e := service.Resolve(ctx, key)
			if e != nil || string(got.Value) != `"fallback"` {
				t.Fatal(key, got, e)
			}
		}
		got, e := service.Resolve(ctx, "EmptyArray")
		if e != nil || string(got.Value) != `""` {
			t.Fatal(got, e)
		}
	})
	t.Run("configuration-overrides-cache-update", func(t *testing.T) {
		key := "Channels:0:Name"
		_, _ = service.Resolve(ctx, key)
		version, e := service.Update(ctx, allow(true), key, json.RawMessage(`"tenant"`))
		if e != nil {
			t.Fatal(e)
		}
		got, e := service.Resolve(ctx, key)
		if e != nil || string(got.Value) != `"tenant"` || got.Version != version {
			t.Fatal(got, e)
		}
		_, e = base.Put(ctx, Layer{Ref: binding.Ref, User: binding.Principal}, key, json.RawMessage(`"user"`))
		if e != nil {
			t.Fatal(e)
		}
		got, e = service.Resolve(ctx, key)
		if e != nil || string(got.Value) != `"user"` {
			t.Fatal(got, e)
		}
		otherReader, _ := config.ParseJSON([]byte(`{"Settings":{"Channels":[{"Name":"changed"}]}}`))
		otherSource, _ := WithConfiguration(NewMemoryProvider(nil), otherReader, nil)
		other, _ := New(otherSource, memory, nil, time.Hour, definitions...)
		got, e = other.Resolve(ctx, key)
		if e != nil || string(got.Value) != `"changed"` {
			t.Fatal("configuration cache revision", got, e)
		}
	})
}

func TestConfigurationProviderErrors(t *testing.T) {
	r, _ := config.ParseJSON([]byte(`{"Settings":{"k":"x"}}`))
	base := NewMemoryProvider(nil)
	if _, e := WithConfiguration(nil, r, nil); e == nil {
		t.Fatal("nil base")
	}
	if _, e := WithConfiguration(base, nil, nil); e == nil {
		t.Fatal("nil reader")
	}
	if _, e := WithConfiguration(base, r, map[string]func(string) (json.RawMessage, error){"k": nil}); e == nil {
		t.Fatal("nil converter")
	}
	p, _ := WithConfiguration(base, r, map[string]func(string) (json.RawMessage, error){"k": func(string) (json.RawMessage, error) { return nil, errors.New("invalid type") }})
	if _, e := p.Read(context.Background(), []Layer{{}}, "k"); e == nil {
		t.Fatal("conversion")
	}
	p, _ = WithConfiguration(base, r, map[string]func(string) (json.RawMessage, error){"k": func(string) (json.RawMessage, error) { return json.RawMessage(`bad`), nil }})
	if _, e := p.Read(context.Background(), []Layer{{}}, "k"); e == nil {
		t.Fatal("invalid json")
	}
}

func TestJSONConfigurationScopeInheritance(t *testing.T) {
	r, _ := config.ParseJSON([]byte(`{"Settings":{"Channels":[{"Name":"configuration"}]}}`))
	base := NewMemoryProvider(nil)
	source, _ := WithConfiguration(base, r, nil)
	memory, _ := cache.NewMemory("app", "v", 8)
	a, b := contextFor(t, "org.organization", "a"), contextFor(t, "org.organization", "b")
	binding, _ := scope.Current(a)
	service, _ := New(source, memory, parent{}, time.Hour, Definition{Key: "Channels:0:Name", Default: json.RawMessage(`"default"`), Inherit: true}, Definition{Key: "local", Default: json.RawMessage(`"local-default"`)})
	_, err := source.Put(a, Layer{Ref: binding.Ref}, "Channels:0:Name", json.RawMessage(`"org-a"`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ctx  context.Context
		want string
	}{{a, `"org-a"`}, {b, `"configuration"`}} {
		got, e := service.Resolve(tc.ctx, "Channels:0:Name")
		if e != nil || string(got.Value) != tc.want {
			t.Fatal(got, e)
		}
	}
	got, e := service.Resolve(a, "local")
	if e != nil || string(got.Value) != `"local-default"` {
		t.Fatal(got, e)
	}
}

type precedenceStep struct {
	layer Layer
	value string
}

func rawStrings(values []json.RawMessage) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

// The full standard layer stack resolves exactly Default < Config < Global <
// Tenant < Enterprise < Organization < User, whatever trusted scope kinds
// participate: five layers tenant-only, six for enterprise or standalone
// organizations, seven for organizations under an enterprise.
func TestSettingsExactLayerPrecedence(t *testing.T) {
	reader, err := config.ParseJSON([]byte(`{"Settings":{"grade":"config"}}`))
	if err != nil {
		t.Fatal(err)
	}
	tenantRef := scope.ScopeRef{TenantID: "tenant", Kind: "tenant", ID: "tenant"}
	enterpriseRef := scope.ScopeRef{TenantID: "tenant", Kind: "org.enterprise", ID: "enterprise"}
	orgRef := scope.ScopeRef{TenantID: "tenant", Kind: "org.organization", ID: "org"}
	for _, tc := range []struct {
		name     string
		kind     string
		id       string
		ancestry Ancestry
		steps    []precedenceStep
		want     []string
	}{
		{
			name: "tenant-five-standard-layers", kind: "tenant", id: "tenant", ancestry: nil,
			steps: []precedenceStep{{Layer{}, `"global"`}, {Layer{Ref: tenantRef}, `"tenant"`}},
			want:  []string{`"user"`, `"tenant"`, `"global"`, `"config"`, `"default"`},
		},
		{
			name: "enterprise-six-standard-layers", kind: "org.enterprise", id: "enterprise", ancestry: nil,
			steps: []precedenceStep{{Layer{}, `"global"`}, {Layer{Ref: tenantRef}, `"tenant"`}, {Layer{Ref: enterpriseRef}, `"enterprise"`}},
			want:  []string{`"user"`, `"enterprise"`, `"tenant"`, `"global"`, `"config"`, `"default"`},
		},
		{
			name: "standalone-organization-six-standard-layers", kind: "org.organization", id: "org", ancestry: parent{},
			steps: []precedenceStep{{Layer{}, `"global"`}, {Layer{Ref: tenantRef}, `"tenant"`}, {Layer{Ref: orgRef}, `"organization"`}},
			want:  []string{`"user"`, `"organization"`, `"tenant"`, `"global"`, `"config"`, `"default"`},
		},
		{
			name: "organization-under-enterprise-seven-standard-layers", kind: "org.organization", id: "org", ancestry: parent{ref: enterpriseRef},
			steps: []precedenceStep{{Layer{}, `"global"`}, {Layer{Ref: tenantRef}, `"tenant"`}, {Layer{Ref: enterpriseRef}, `"enterprise"`}, {Layer{Ref: orgRef}, `"organization"`}},
			want:  []string{`"user"`, `"organization"`, `"enterprise"`, `"tenant"`, `"global"`, `"config"`, `"default"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := contextFor(t, tc.kind, tc.id)
			b, _ := scope.Current(ctx)
			base := NewMemoryProvider(nil)
			source, err := WithConfiguration(base, reader, nil)
			if err != nil {
				t.Fatal(err)
			}
			memory, _ := cache.NewMemory("app", "v", 32)
			s, err := New(source, memory, tc.ancestry, time.Minute, Definition{Key: "grade", Default: json.RawMessage(`"default"`), Inherit: true})
			if err != nil {
				t.Fatal(err)
			}
			expect := []string{`"config"`, `"default"`}
			got, err := s.Resolve(ctx, "grade")
			if err != nil || string(got.Value) != `"config"` {
				t.Fatal("config is the first standard layer above default", got, err)
			}
			for _, step := range tc.steps {
				if _, err = base.Put(ctx, step.layer, "grade", json.RawMessage(step.value)); err != nil {
					t.Fatal(err)
				}
				expect = append([]string{step.value}, expect...)
				got, err = s.Resolve(ctx, "grade")
				if err != nil || string(got.Value) != step.value {
					t.Fatal(step.value, got, err)
				}
				if candidates := rawStrings(got.Candidates); !slices.Equal(candidates, expect) {
					t.Fatal(step.value, "candidates must follow the exact standard order", candidates, expect)
				}
			}
			if _, err = base.Put(ctx, Layer{Ref: b.Ref, User: b.Principal}, "grade", json.RawMessage(`"user"`)); err != nil {
				t.Fatal(err)
			}
			got, err = s.Resolve(ctx, "grade")
			if err != nil || string(got.Value) != `"user"` {
				t.Fatal("user layer must outrank every scope layer", got, err)
			}
			if candidates := rawStrings(got.Candidates); !slices.Equal(candidates, tc.want) {
				t.Fatal("exact standard layer order", candidates, tc.want)
			}
		})
	}
}

// Config and Global are independent layers: Global stores mutable values at the
// empty Layer address, Config resolves Settings:+name from the reader, Global
// overrides Config, and updates on either side stay visible and isolated.
func TestSettingsConfigGlobalCoexistence(t *testing.T) {
	ctx := contextFor(t, "tenant", "tenant")
	base := NewMemoryProvider(nil)
	reader, err := config.ParseJSON([]byte(`{"Settings":{"mode":"config-mode"}}`))
	if err != nil {
		t.Fatal(err)
	}
	source, _ := WithConfiguration(base, reader, nil)
	c, _ := cache.NewMemory("app", "v", 16)
	definitions := []Definition{
		{Key: "mode", Default: json.RawMessage(`"default-mode"`), Inherit: true},
		{Key: "flat", Default: json.RawMessage(`"flat-default"`)},
		{Key: "tone", Default: json.RawMessage(`"tone-default"`), Inherit: true},
	}
	s, err := New(source, c, nil, time.Minute, definitions...)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Resolve(ctx, "tone")
	if err != nil || string(got.Value) != `"tone-default"` {
		t.Fatal("no-value fallback must be the declared default", got, err)
	}
	got, err = s.Resolve(ctx, "mode")
	if err != nil || string(got.Value) != `"config-mode"` {
		t.Fatal("config is the first standard layer above default", got, err)
	}
	if _, err = base.Put(ctx, Layer{}, "mode", json.RawMessage(`"global-mode"`)); err != nil {
		t.Fatal(err)
	}
	got, err = s.Resolve(ctx, "mode")
	if err != nil || string(got.Value) != `"global-mode"` {
		t.Fatal("global must override config", got, err)
	}
	if _, err = base.Put(ctx, Layer{}, "mode", json.RawMessage(`"global-two"`)); err != nil {
		t.Fatal(err)
	}
	got, err = s.Resolve(ctx, "mode")
	if err != nil || string(got.Value) != `"global-two"` {
		t.Fatal("a global update must invalidate the cached version even without a cache delete", got, err)
	}
	otherReader, _ := config.ParseJSON([]byte(`{"Settings":{"mode":"config-two"}}`))
	otherSource, _ := WithConfiguration(base, otherReader, nil)
	other, _ := New(otherSource, c, nil, time.Minute, definitions...)
	got, err = other.Resolve(ctx, "mode")
	if err != nil || string(got.Value) != `"global-two"` {
		t.Fatal("global still overrides the changed config", got, err)
	}
	if slices.Contains(rawStrings(got.Candidates), `"config-mode"`) {
		t.Fatal("stale config candidate served from cache", got)
	}
	if !slices.Contains(rawStrings(got.Candidates), `"config-two"`) {
		t.Fatal("changed config candidate missing", got)
	}
	if _, err = base.Put(ctx, Layer{}, "flat", json.RawMessage(`"global-flat"`)); err != nil {
		t.Fatal(err)
	}
	got, err = s.Resolve(ctx, "flat")
	if err != nil || string(got.Value) != `"flat-default"` {
		t.Fatal("non-inheriting definitions must ignore global and config", got, err)
	}
}

// Configuration lookup is the exact Settings:+name key: entries outside the
// Settings namespace are never read and setting names are never rewritten.
func TestConfigurationExactSettingsNamespace(t *testing.T) {
	ctx := contextFor(t, "tenant", "tenant")
	base := NewMemoryProvider(nil)
	reader, err := config.ParseJSON([]byte(`{"Settings":{"grade":"scoped"},"grade":"bare"}`))
	if err != nil {
		t.Fatal(err)
	}
	source, _ := WithConfiguration(base, reader, nil)
	c, _ := cache.NewMemory("app", "v", 16)
	s, _ := New(source, c, nil, time.Minute, Definition{Key: "grade", Default: json.RawMessage(`"default"`), Inherit: true})
	got, err := s.Resolve(ctx, "grade")
	if err != nil || string(got.Value) != `"scoped"` {
		t.Fatal("lookup must use the exact Settings:+name key", got, err)
	}
	bareReader, _ := config.ParseJSON([]byte(`{"grade":"bare"}`))
	bareSource, _ := WithConfiguration(NewMemoryProvider(nil), bareReader, nil)
	bare, _ := New(bareSource, c, nil, time.Minute, Definition{Key: "grade", Default: json.RawMessage(`"default"`), Inherit: true})
	got, err = bare.Resolve(ctx, "grade")
	if err != nil || string(got.Value) != `"default"` {
		t.Fatal("configuration outside the Settings namespace must not be read", got, err)
	}
}

// Within one snapshot the Global value and the converted Config value remain
// two distinct entries, Global first: Config never stores through the Global
// address and never outranks it.
func TestConfigurationBelowGlobal(t *testing.T) {
	base := NewMemoryProvider(nil)
	if _, err := base.Put(context.Background(), Layer{}, "k", json.RawMessage(`"global"`)); err != nil {
		t.Fatal(err)
	}
	reader, err := config.ParseJSON([]byte(`{"Settings":{"k":"config"}}`))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := WithConfiguration(base, reader, nil)
	snapshot, err := p.Read(context.Background(), []Layer{{}}, "k")
	if err != nil {
		t.Fatal(err)
	}
	if got := rawStrings(snapshot.Values); !slices.Equal(got, []string{`"global"`, `"config"`}) {
		t.Fatal("config must be a distinct layer below global", got)
	}
}
