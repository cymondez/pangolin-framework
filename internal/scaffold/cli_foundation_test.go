// Generator regression for foundation-based composition: framework dependency
// names translate into the foundation Type edge, and a generated user module
// receives framework services through typed constructor injection, verified by
// building the generated project sources.
package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const catalogServiceGo = `package catalog

import (
	"github.com/cymondez/pangolin-framework/events/outbox"
	"github.com/cymondez/pangolin-framework/persistence/idempotency"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"example.com/bookstore/foundation"
)

// CatalogService consumes the published framework contracts strictly by type:
// typed constructor injection, no low-level tokens and no app-root import.
type CatalogService struct {
	units   uow.UnitOfWorkManager
	scopes  foundation.ScopeAccess
	publish *outbox.Publisher
	idem    *idempotency.Store
}

func NewCatalogService(units uow.UnitOfWorkManager, scopes foundation.ScopeAccess, publish *outbox.Publisher, idem *idempotency.Store) *CatalogService {
	return &CatalogService{units: units, scopes: scopes, publish: publish, idem: idem}
}
`

// TestGeneratedTypedFrameworkInjection builds generated projects in which a
// user module declares framework dependency names and injects the published
// foundation contracts through its constructor; both profiles must compile.
func TestGeneratedTypedFrameworkInjection(t *testing.T) {
	for _, withOrg := range []bool{false, true} {
		dir := create(t, withOrg)
		if code, out := invoke(t, dir, "module", "add", "catalog", "--depends", "persistence,authorization"); code != 0 {
			t.Fatalf("%s: %s", dir, out)
		}
		if code, out := invoke(t, dir, "module", "add", "prices", "--depends", "catalog"); code != 0 {
			t.Fatalf("%s chained module add: %s", dir, out)
		}
		module, err := os.ReadFile(filepath.Join(dir, "modules", "catalog", "module.go"))
		if err != nil {
			t.Fatal(err)
		}
		raw := string(module)
		if !strings.Contains(raw, "module.Of[foundation.Module]()") || !strings.Contains(raw, "example.com/bookstore/foundation") {
			t.Fatalf("%s: generated module misses the foundation Type edge:\n%s", dir, raw)
		}
		prices, err := os.ReadFile(filepath.Join(dir, "modules", "prices", "module.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(prices), "module.Of[catalog.Module]()") {
			t.Fatalf("%s: chained Type edge missing:\n%s", dir, prices)
		}
		if err = os.WriteFile(filepath.Join(dir, "modules", "catalog", "service.go"), []byte(catalogServiceGo), 0o644); err != nil {
			t.Fatal(err)
		}
		// Wire the typed constructor exactly the way the generated Configure
		// comment suggests: an ordinary Add, nothing low-level.
		wired := strings.Replace(raw, "func (m *Module) Configure(b module.RootBuilder) error { return nil }", "func (m *Module) Configure(b module.RootBuilder) error {\n\treturn b.Services().Add(NewCatalogService, module.Singleton())\n}", 1)
		if wired == raw {
			t.Fatalf("%s: generated Configure line not found:\n%s", dir, raw)
		}
		if err = os.WriteFile(filepath.Join(dir, "modules", "catalog", "module.go"), []byte(wired), 0o644); err != nil {
			t.Fatal(err)
		}
		build := exec.Command("go", "-C", dir, "build", "./...")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("%s: generated typed injection project does not build:\n%s", dir, out)
		}
		vet := exec.Command("go", "-C", dir, "vet", "./modules/...")
		if out, err := vet.CombinedOutput(); err != nil {
			t.Fatalf("%s: generated typed injection project does not vet:\n%s", dir, out)
		}
	}
}
