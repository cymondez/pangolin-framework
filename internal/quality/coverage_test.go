package quality

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestCoverageThresholdUsesRawCounts(t *testing.T) {
	for _, covered := range []int64{8999, 9000} {
		t.Run(fmt.Sprint(covered), func(t *testing.T) {
			profile := fmt.Sprintf("mode: atomic\nx/p/a.go:1.1,2.1 %d 1\nx/p/a.go:3.1,4.1 %d 0\n", covered, 10000-covered)
			got, err := CountCoverage([]io.Reader{strings.NewReader(profile)}, []string{"x/p/a.go"})
			if err != nil {
				t.Fatal(err)
			}
			if got.Overall.Pass() != (covered == 9000) || got.Overall.Total != 10000 {
				t.Fatalf("wrong counts: %+v", got)
			}
		})
	}
	if (Counts{}).Pass() {
		t.Fatal("no statements must not be reported as 100%")
	}
}

func TestCoverageUnionAndUntestedPackage(t *testing.T) {
	a := "mode: atomic\nx/p/a.go:1.1,2.1 8 1\nx/q/b.go:1.1,2.1 2 0\n"
	b := "mode: atomic\nx/p/a.go:1.1,2.1 8 99\nx/q/b.go:1.1,2.1 2 1\n"
	got, err := CountCoverage([]io.Reader{strings.NewReader(a), strings.NewReader(b)}, []string{"x/p/a.go", "x/q/b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Overall != (Counts{10, 10}) {
		t.Fatalf("duplicate blocks counted twice: %+v", got)
	}
	got, err = CountCoverage([]io.Reader{strings.NewReader(a)}, []string{"x/p/a.go", "x/q/b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Overall.Pass() || got.Packages["x/q"].Covered != 0 {
		t.Fatalf("untested package disappeared: %+v", got)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func TestCoverageRejectsMissingOrCorruptEvidence(t *testing.T) {
	valid := "mode: atomic\nx/p/a.go:1.1,2.1 1 1\n"
	tests := []struct {
		name     string
		profiles []io.Reader
		files    []string
	}{
		{"no profiles", nil, []string{"x/p/a.go"}},
		{"no inventory", []io.Reader{strings.NewReader(valid)}, nil},
		{"empty", []io.Reader{strings.NewReader("")}, []string{"x/p/a.go"}},
		{"wrong mode", []io.Reader{strings.NewReader("mode: set\n")}, []string{"x/p/a.go"}},
		{"corrupt block", []io.Reader{strings.NewReader("mode: atomic\nbad\n")}, []string{"x/p/a.go"}},
		{"missing file", []io.Reader{strings.NewReader(valid)}, []string{"x/p/a.go", "x/q/b.go"}},
		{"unknown file", []io.Reader{strings.NewReader(valid)}, []string{"x/q/b.go"}},
		{"different metadata", []io.Reader{strings.NewReader(valid), strings.NewReader(strings.Replace(valid, " 1 1", " 2 1", 1))}, []string{"x/p/a.go"}},
		{"statement overflow", []io.Reader{strings.NewReader(strings.Replace(valid, " 1 1", " 99999999999999 1", 1))}, []string{"x/p/a.go"}},
		{"count overflow", []io.Reader{strings.NewReader(strings.Replace(valid, " 1 1", " 1 99999999999999999999999", 1))}, []string{"x/p/a.go"}},
		{"read error", []io.Reader{io.MultiReader(strings.NewReader("mode: atomic\n"), failingReader{})}, []string{"x/p/a.go"}},
		{"missing package", []io.Reader{strings.NewReader("mode: atomic\na.go:1.1,2.1 1 1\n")}, []string{"a.go"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CountCoverage(test.profiles, test.files); err == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
}
