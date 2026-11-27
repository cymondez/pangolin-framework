package config

import (
	"errors"
	"strings"
	"testing"
)

func TestJSONConfiguration(t *testing.T) {
	t.Run("arrays-and-literal-paths", func(t *testing.T) {
		r, err := ParseJSON([]byte(`{"Settings":{"Channels":[{"Name":"mail"},{"Name":"sms"}],"Matrix":[[1,2],[3]],"App.Mailing.Smtp.Host":"literal","App":{"Mailing":{"Smtp":{"Host":"nested"}}},"0":{"Name":"property"}}}`))
		if err != nil {
			t.Fatal(err)
		}
		for k, want := range map[string]string{"Settings:Channels:0:Name": "mail", "settings:channels:1:name": "sms", "Settings:Matrix:0:1": "2", "Settings:Matrix:1:0": "3", "Settings:App.Mailing.Smtp.Host": "literal", "Settings:App:Mailing:Smtp:Host": "nested", "Settings:0:Name": "property"} {
			if got, ok := r.Lookup(k); !ok || got != want {
				t.Fatal(k, got, ok)
			}
		}
		if _, ok := r.Lookup("Settings:Channels[0].Name"); ok {
			t.Fatal("rewritten setting key")
		}
	})
	t.Run("double-digit-index", func(t *testing.T) {
		r, err := ParseJSON([]byte(`{"Items":[0,1,2,3,4,5,6,7,8,9,10,11]}`))
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := r.Lookup("items:11"); v != "11" {
			t.Fatal(v)
		}
	})
	t.Run("scalars-null-and-empty", func(t *testing.T) {
		r, err := ParseJSON([]byte(`{"Null":null,"Object":{},"Array":[],"Empty":"","Bool":true,"No":false,"Number":1.20e+2,"Text":"null","Url":"https://example.com/a//b"}`))
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"Null", "Object", "missing"} {
			if _, ok := r.Lookup(k); ok {
				t.Fatal(k)
			}
		}
		for k, want := range map[string]string{"Array": "", "Empty": "", "Bool": "True", "No": "False", "Number": "1.20e+2", "Text": "null", "Url": "https://example.com/a//b"} {
			if v, ok := r.Lookup(k); !ok || v != want {
				t.Fatal(k, v, ok)
			}
		}
	})
	t.Run("duplicate-and-flattening-collision", func(t *testing.T) {
		for _, input := range []string{`{"A":1,"a":2}`, `{"a:b":1,"a":{"b":2}}`, `{"a":null,"A":1}`, `{"a":{},"A":1}`} {
			if _, err := ParseJSON([]byte(input)); !errors.Is(err, ErrDuplicateKey) {
				t.Fatal(input, err)
			}
		}
		r, err := ParseJSON([]byte(`{"a":1,"A":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := r.Lookup("a"); ok {
			t.Fatal("empty-object assignment semantics")
		}
		r, err = ParseJSON([]byte(`{"a":1,"A":[]}`))
		if err != nil {
			t.Fatal(err)
		}
		if v, ok := r.Lookup("a"); !ok || v != "" {
			t.Fatal("empty-array assignment semantics")
		}
	})
	t.Run("comments-trailing-comma-and-bom", func(t *testing.T) {
		input := append([]byte{0xef, 0xbb, 0xbf}, []byte("{/* block */\"a\": [1,2,],// line\n\"b\":\"a\\\"//b\",}")...)
		r, err := ParseJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := r.Lookup("a:1"); v != "2" {
			t.Fatal(v)
		}
	})
	t.Run("ordinal-casing", func(t *testing.T) {
		r, err := ParseJSON([]byte(`{"k":"ascii","K":"kelvin","s":"ascii-s","ſ":"long-s","i":"ascii-i","ı":"dotless","σ":"sigma","é":"accent","ß":"sharp","ẞ":"capital-sharp"}`))
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{"K": "ascii", "K": "kelvin", "S": "ascii-s", "ſ": "long-s", "I": "ascii-i", "ı": "dotless", "ς": "sigma", "É": "accent", "ß": "sharp", "ẞ": "capital-sharp"} {
			if got, _ := r.Lookup(key); got != want {
				t.Fatal(key, got)
			}
		}
	})
	t.Run("invalid-and-depth", func(t *testing.T) {
		for _, input := range []string{"", `[]`, `null`, `true`, `{"a":1} {}`, `{"a":}`, `{"a":[,]}`, `{"a":[1,,]}`, `{/* unterminated`, string([]byte{'{', '"', 'a', '"', ':', '"', 0xff, '"', '}'})} {
			if _, err := ParseJSON([]byte(input)); err == nil {
				t.Fatal(input)
			}
		}
		if _, err := ParseJSON([]byte(strings.Repeat(`{"a":`, 65) + `0` + strings.Repeat(`}`, 65))); err == nil {
			t.Fatal("unbounded depth")
		}
		if _, err := ParseJSON([]byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	})
}
