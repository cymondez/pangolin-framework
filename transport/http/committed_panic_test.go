package http

import (
	"context"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type panicJSON struct{}

func (panicJSON) MarshalJSON() ([]byte, error) { panic("serialization panic") }
func TestCommittedSerializationPanicRetainsOutcome(t *testing.T) {
	tx := &transaction{}
	m, e := uow.NewManager(resource{tx}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 1})
	if e != nil {
		t.Fatal(e)
	}
	ctx := bound(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/write", nil).WithContext(ctx)
	func() {
		defer func() {
			if v := recover(); v != nil {
				t.Fatalf("serialization panic escaped with committed transaction: %v", v)
			}
		}()
		Command(m, func(context.Context) (any, error) { return panicJSON{}, nil }).ServeHTTP(w, r)
	}()
	if !tx.committed || w.Code != 500 || !strings.Contains(w.Body.String(), `"outcome":"committed"`) || !strings.Contains(w.Body.String(), `"operationId":`) {
		t.Fatal(tx.committed, w.Body.String())
	}
}
