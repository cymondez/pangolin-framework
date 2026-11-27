package audit

import (
	"context"
	"errors"
	"github.com/pangolin-framework/pangolin/security/scope"
	"testing"
)

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }
func TestAuditRequiredScopeAndIndependentWriterErrors(t *testing.T) {
	r := Record{Action: "books.write", Resource: "opaque-id", Result: "failed", Trace: "trace-1"}
	if err := (Recorder{}).Record(context.Background(), r); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "a", true
	})
	ctx, _ := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"})
	if err := (Recorder{}).Record(ctx, r); err == nil {
		t.Fatal("audit outside UOW")
	}
	boom := errors.New("log unavailable")
	l, _ := NewSecurityLog(failWriter{boom})
	if !errors.Is(l.Write(ctx, r), boom) {
		t.Fatal("writer error discarded")
	}
	r.Duration = -1
	if !errors.Is(l.Write(ctx, r), ErrInvalid) {
		t.Fatal("invalid duration")
	}
}
