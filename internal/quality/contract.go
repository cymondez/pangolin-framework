// Package quality implements repository engineering checks, not runtime acceptance.
package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Case struct {
	ID          string `json:"id"`
	Requirement string `json:"requirement"`
	Milestone   string `json:"milestone"`
	Status      string `json:"status"`
}

type Manifest struct {
	Baseline string `json:"baseline"`
	Cases    []Case `json:"cases"`
}

var (
	requirementRE = regexp.MustCompile(`(?m)^- ([A-Z]+-[0-9]{3}) —`)
	caseRE        = regexp.MustCompile(`(?m)^\| (A[0-9]{2}) \| ([A-Z]+-[0-9]{3}) \|`)
	deliveryRE    = regexp.MustCompile(`(?m)^\| (M[0-6]) \| ([A-Z]+-[0-9]{3}) \| (A[0-9]{2})；`)
)

// ValidateContract verifies the three independently maintained requirement maps.
// Planned cases remain planned; this never runs or certifies a runtime case.
func ValidateContract(spec, plan []byte, manifest Manifest, release bool) error {
	if manifest.Baseline != "v1.3" {
		return fmt.Errorf("unsupported contract baseline %q", manifest.Baseline)
	}
	requirements := make(map[string]bool)
	for _, match := range requirementRE.FindAllSubmatch(spec, -1) {
		id := string(match[1])
		if requirements[id] {
			return fmt.Errorf("duplicate requirement %s", id)
		}
		requirements[id] = true
	}
	if len(requirements) != 95 {
		return fmt.Errorf("expected 95 requirements, got %d", len(requirements))
	}
	cases := make(map[string]string)
	mapped := make(map[string]bool)
	for _, match := range caseRE.FindAllSubmatch(spec, -1) {
		id, req := string(match[1]), string(match[2])
		if cases[id] != "" || mapped[req] || !requirements[req] {
			return fmt.Errorf("invalid or duplicate acceptance mapping %s/%s", id, req)
		}
		cases[id], mapped[req] = req, true
	}
	for n := 1; n <= 95; n++ {
		if cases[fmt.Sprintf("A%02d", n)] == "" {
			return fmt.Errorf("missing acceptance case A%02d", n)
		}
	}
	deliveries := make(map[string]string)
	for _, match := range deliveryRE.FindAllSubmatch(plan, -1) {
		milestone, req, id := string(match[1]), string(match[2]), string(match[3])
		if deliveries[id] != "" || cases[id] != req {
			return fmt.Errorf("invalid or duplicate delivery mapping %s/%s", id, req)
		}
		deliveries[id] = milestone
	}
	if len(deliveries) != 95 || len(manifest.Cases) != 95 {
		return fmt.Errorf("expected 95 deliveries and manifest cases, got %d/%d", len(deliveries), len(manifest.Cases))
	}
	seen := make(map[string]bool)
	for _, c := range manifest.Cases {
		if seen[c.ID] || cases[c.ID] != c.Requirement || deliveries[c.ID] != c.Milestone {
			return fmt.Errorf("manifest drift at %s", c.ID)
		}
		seen[c.ID] = true
		// Runtime results must come from a future evidence collector, never a
		// hand-edited manifest flag. Even "verified" is deliberately rejected.
		if c.Status != "planned" {
			return fmt.Errorf("case %s: skeleton status must remain planned", c.ID)
		}
	}
	if release {
		return fmt.Errorf("release blocked: all 95 runtime case groups await executable tests and same-commit evidence")
	}
	return nil
}

func CheckRepository(root string, release bool) error {
	spec, err := os.ReadFile(filepath.Join(root, "design", "panGOlin-framework_實作規範書.md"))
	if err != nil {
		return err
	}
	plan, err := os.ReadFile(filepath.Join(root, "design", "panGOlin-framework_里程碑規劃書.md"))
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, "tests", "contract", "cases.json"))
	if err != nil {
		return err
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return err
	}
	return ValidateContract(spec, plan, manifest, release)
}
