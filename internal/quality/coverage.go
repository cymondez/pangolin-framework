package quality

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type Counts struct {
	Covered int64 `json:"covered_statements"`
	Total   int64 `json:"total_statements"`
}

func (c Counts) Pass() bool { return c.Total > 0 && c.Covered*100 >= c.Total*90 }

type Coverage struct {
	Overall  Counts            `json:"overall"`
	Packages map[string]Counts `json:"packages"`
}

type block struct {
	file       string
	statements int64
	covered    bool
}

var blockRE = regexp.MustCompile(`^(.+):([0-9]+\.[0-9]+,[0-9]+\.[0-9]+) ([0-9]+) ([0-9]+)$`)

// CountCoverage unions statement blocks from profiles belonging to one source
// and build. Callers must establish build identity before merging profiles.
// Expected files come from the build's go-list inventory, so an absent package
// cannot disappear from the denominator. Empty/declaration-only files should
// not be included in expectedFiles.
func CountCoverage(profiles []io.Reader, expectedFiles []string) (Coverage, error) {
	result := Coverage{Packages: make(map[string]Counts)}
	if len(profiles) == 0 || len(expectedFiles) == 0 {
		return result, fmt.Errorf("coverage requires profiles and an executable-source inventory")
	}
	expected := make(map[string]bool)
	for _, file := range expectedFiles {
		expected[file] = true
	}
	blocks := make(map[string]block)
	observed := make(map[string]bool)
	for _, profile := range profiles {
		scanner := bufio.NewScanner(profile)
		if !scanner.Scan() || scanner.Text() != "mode: atomic" {
			return result, fmt.Errorf("coverage profile must use atomic mode")
		}
		for scanner.Scan() {
			match := blockRE.FindStringSubmatch(scanner.Text())
			if match == nil {
				return result, fmt.Errorf("invalid coverage block %q", scanner.Text())
			}
			statements, err := strconv.ParseInt(match[3], 10, 32)
			if err != nil {
				return result, err
			}
			count, err := strconv.ParseInt(match[4], 10, 64)
			if err != nil {
				return result, err
			}
			file, key := match[1], match[1]+":"+match[2]
			if !expected[file] {
				return result, fmt.Errorf("unexpected source in coverage profile: %s", file)
			}
			previous, exists := blocks[key]
			if exists && previous.statements != statements {
				return result, fmt.Errorf("inconsistent statement metadata at %s", key)
			}
			blocks[key] = block{file, statements, previous.covered || count > 0}
			observed[file] = true
		}
		if err := scanner.Err(); err != nil {
			return result, err
		}
	}
	for file := range expected {
		if !observed[file] {
			return result, fmt.Errorf("missing instrumentation/profile for %s", file)
		}
	}
	for _, b := range blocks {
		index := strings.LastIndex(b.file, "/")
		if index < 0 {
			return result, fmt.Errorf("source lacks package path: %s", b.file)
		}
		pkg := b.file[:index]
		counts := result.Packages[pkg]
		counts.Total += b.statements
		result.Overall.Total += b.statements
		if b.covered {
			counts.Covered += b.statements
			result.Overall.Covered += b.statements
		}
		result.Packages[pkg] = counts
	}
	return result, nil
}
