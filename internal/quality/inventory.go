package quality

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// SourceInventory consumes go list -json from the current platform/build.
// Only explicitly enumerated fixture files may be excluded in M0.
func SourceInventory(data io.Reader, exclusions map[string]string) ([]string, error) {
	decoder := json.NewDecoder(data)
	files := make([]string, 0)
	for {
		var pkg struct {
			ImportPath string
			Dir        string
			GoFiles    []string
			CgoFiles   []string
		}
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		for _, name := range append(pkg.GoFiles, pkg.CgoFiles...) {
			key := pkg.ImportPath + "/" + name
			if reason, exists := exclusions[key]; exists {
				if reason != "test fixture" {
					return nil, fmt.Errorf("unsupported coverage exclusion for %s", key)
				}
				continue
			}
			source, err := os.ReadFile(filepath.Join(pkg.Dir, name))
			if err != nil {
				return nil, err
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
			if err != nil {
				return nil, err
			}
			executable := false
			ast.Inspect(parsed, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.FuncDecl:
					executable = executable || node.Body != nil && len(node.Body.List) > 0
				case *ast.FuncLit:
					executable = executable || len(node.Body.List) > 0
				}
				return true
			})
			if executable {
				files = append(files, key)
			}
		}
	}
	sort.Strings(files)
	return files, nil
}
