package quality

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryIncludesUntestedSourceAndExplicitFixtures(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"decl.go": "package p; type P struct{}", "code.go": "package p; func f() int { return 1 }", "fixture.go": "package p; func fixture() { panic(1) }", "literal.go": "package p; var f = func() int { return 1 }"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(map[string]any{"ImportPath": "x/p", "Dir": root, "GoFiles": []string{"decl.go", "code.go", "fixture.go", "literal.go"}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := SourceInventory(strings.NewReader(string(data)), map[string]string{"x/p/fixture.go": "test fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != "x/p/code.go,x/p/literal.go" {
		t.Fatalf("wrong inventory: %v", files)
	}
	for _, test := range []struct {
		data       string
		exclusions map[string]string
	}{
		{"{", nil},
		{string(data), map[string]string{"x/p/code.go": "hard to test"}},
		{`{"ImportPath":"x/p","Dir":"` + filepath.ToSlash(root) + `","GoFiles":["missing.go"]}`, nil},
	} {
		if _, err := SourceInventory(strings.NewReader(test.data), test.exclusions); err == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
	if err := os.WriteFile(filepath.Join(root, "code.go"), []byte("not go"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := SourceInventory(strings.NewReader(string(data)), nil); err == nil {
		t.Fatal("invalid source accepted")
	}
}
