package http

import (
	"bytes"
	"context"
	"github.com/cymondez/pangolin-framework/platform/audit"
	"github.com/cymondez/pangolin-framework/security/scope"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type verifiedActorKey struct{}

func TestA62AuthenticatedDenialRetainsActor(t *testing.T) {
	for _, stage := range []string{"tenant", "scope"} {
		t.Run(stage, func(t *testing.T) {
			var log bytes.Buffer
			identity := func(c context.Context) (scope.PrincipalRef, string, bool) {
				id, ok := c.Value(verifiedActorKey{}).(string)
				return scope.PrincipalRef{ID: id, Kind: "service"}, "a", ok
			}
			security, e := audit.NewSecurityLog(&log, identity)
			if e != nil {
				t.Fatal(e)
			}
			o := pipelineOptions(t)
			o.Authenticate = func(c context.Context, _ string) (context.Context, error) {
				return context.WithValue(c, verifiedActorKey{}, "verified-alice"), nil
			}
			reject := func(context.Context, *http.Request) (context.Context, error) { return nil, scope.ErrDenied }
			if stage == "tenant" {
				o.ResolveTenant = reject
			} else {
				o.ResolveScope = reject
			}
			o.Rejected = func(c context.Context, _ error) {
				if e := security.Write(c, audit.Record{Action: "books.read", Resource: "opaque", Result: "denied", Trace: "trace-1"}); e != nil {
					t.Error(e)
				}
			}
			p, e := NewPipeline(o)
			if e != nil {
				t.Fatal(e)
			}
			r := httptest.NewRequest("POST", "/books", strings.NewReader("{}"))
			r.Header.Set("Authorization", "Bearer verified")
			r.Header.Set("X-Scope-ID", "forged-owner")
			w := httptest.NewRecorder()
			p.ServeHTTP(w, r)
			if w.Code != 403 || !strings.Contains(log.String(), "verified-alice") || strings.Contains(log.String(), "forged-owner") {
				t.Fatal(w.Code, log.String())
			}
		})
	}
}
