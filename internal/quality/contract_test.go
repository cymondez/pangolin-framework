package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func contractFixture() (string, string, Manifest) {
	var spec, plan strings.Builder
	manifest := Manifest{Baseline: "v1.3"}
	for n := 1; n <= 95; n++ {
		req, id := fmt.Sprintf("MOD-%03d", n), fmt.Sprintf("A%02d", n)
		fmt.Fprintf(&spec, "- %s — MUST work\n| %s | %s | setup | stimulus | expected |\n", req, id, req)
		fmt.Fprintf(&plan, "| M1 | %s | %s；evidence | Planned |\n", req, id)
		manifest.Cases = append(manifest.Cases, Case{id, req, "M1", "planned"})
	}
	return spec.String(), plan.String(), manifest
}

func TestContractMappingAndReleaseBoundary(t *testing.T) {
	spec, plan, manifest := contractFixture()
	if err := ValidateContract([]byte(spec), []byte(plan), manifest, false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateContract([]byte(spec), []byte(plan), manifest, true); err == nil {
		t.Fatal("a planned inventory must never clear release")
	}
	tests := []struct {
		name string
		edit func(*string, *string, *Manifest)
	}{
		{"baseline", func(s, p *string, m *Manifest) { m.Baseline = "v0" }},
		{"duplicate requirement", func(s, p *string, m *Manifest) { *s += "- MOD-001 — duplicate\n" }},
		{"missing requirement", func(s, p *string, m *Manifest) { *s = strings.ReplaceAll(*s, "- MOD-001 — MUST work\n", "") }},
		{"duplicate case", func(s, p *string, m *Manifest) { *s += "| A01 | MOD-001 | duplicate |\n" }},
		{"unknown requirement", func(s, p *string, m *Manifest) { *s = strings.Replace(*s, "| A01 | MOD-001 |", "| A01 | MOD-999 |", 1) }},
		{"missing case", func(s, p *string, m *Manifest) { *s = strings.Replace(*s, "| A01 |", "| X01 |", 1) }},
		{"duplicate delivery", func(s, p *string, m *Manifest) { *p += "| M2 | MOD-001 | A01；duplicate |\n" }},
		{"wrong delivery", func(s, p *string, m *Manifest) { *p = strings.Replace(*p, "MOD-001", "MOD-002", 1) }},
		{"missing delivery", func(s, p *string, m *Manifest) { *p = "" }},
		{"missing manifest", func(s, p *string, m *Manifest) { m.Cases = m.Cases[1:] }},
		{"manifest drift", func(s, p *string, m *Manifest) { m.Cases[0].Milestone = "M2" }},
		{"duplicate manifest", func(s, p *string, m *Manifest) { m.Cases[0] = m.Cases[1] }},
		{"fake verification", func(s, p *string, m *Manifest) { m.Cases[0].Status = "verified" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, p, m := contractFixture()
			test.edit(&s, &p, &m)
			if err := ValidateContract([]byte(s), []byte(p), m, false); err == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
}

func TestCheckRepositoryFileFailures(t *testing.T) {
	root := t.TempDir()
	spec, plan, manifest := contractFixture()
	files := []struct{ path, content string }{
		{"design/panGOlin-framework_實作規範書.md", spec},
		{"design/panGOlin-framework_里程碑規劃書.md", plan},
		{"tests/contract/cases.json", "{"},
	}
	for _, file := range files {
		if err := CheckRepository(root, false); err == nil {
			t.Fatal("incomplete repository accepted")
		}
		path := filepath.Join(root, file.path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(file.content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := CheckRepository(root, false); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, files[2].path), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := CheckRepository(root, false); err != nil {
		t.Fatal(err)
	}
}
