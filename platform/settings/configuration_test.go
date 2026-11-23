package settings

import (
	"context"
	"encoding/json"
	"errors"
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
