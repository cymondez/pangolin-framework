//go:build integration

package m3

import (
	"context"
	"crypto/rand"
	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/events/inbox"
	eventpg "github.com/cymondez/pangolin-framework/events/postgres"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/security/scope"
	"testing"
	"time"
)

type admissionDenial struct{ calls int }

func (a *admissionDenial) Kinds() []string { return []string{"tenant"} }
func (a *admissionDenial) Resolve(_ context.Context, p scope.PrincipalRef, r scope.ScopeRef) (scope.ScopeBinding, error) {
	a.calls++
	if a.calls > 1 {
		return scope.ScopeBinding{}, scope.ErrAuthorityUnavailable
	}
	return scope.ScopeBinding{Ref: r, Principal: p, AuthorityRevision: "entry"}, nil
}
func (a *admissionDenial) Authorize(context.Context, scope.ScopeBinding, string) error { return nil }
func TestInboxUnauthorizedCandidateDoesNotStarveGroup(t *testing.T) {
	for _, kind := range []string{"bind", "mismatch", "admission"} {
		t.Run(kind, func(t *testing.T) {
			admin, _, worker := eventFixture(t)
			store, _ := eventpg.NewStore(worker, eventPolicy())
			source := eventContext(t, "queue", scope.PrincipalRef{ID: "producer", Kind: "user"})
			first, second := envelope(source, rand.Text()), envelope(source, rand.Text())
			group := rand.Text()
			receiver, _ := inbox.NewReceiver(store, eventRegistry(t), group, func(context.Context, distributed.Envelope) (string, error) { return "m3", nil }, func(context.Context, error) {})
			for _, e := range []distributed.Envelope{first, second} {
				if err := receiver.Receive(context.Background(), e, func(context.Context) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := admin.Exec(context.Background(), `UPDATE pangolin_events.inbox SET created_at=now()-interval '1 hour' WHERE event_id=$1`, first.ID); err != nil {
				t.Fatal(err)
			}
			bind := func(c context.Context, candidate inbox.Candidate) (context.Context, error) {
				if candidate.ID == first.ID {
					switch kind {
					case "bind":
						return nil, scope.ErrAuthorityUnavailable
					case "mismatch":
						return eventContext(t, "other", scope.PrincipalRef{ID: "worker", Kind: "service"}), nil
					case "admission":
						identity := func(context.Context) (scope.PrincipalRef, string, bool) {
							return scope.PrincipalRef{ID: "worker", Kind: "service"}, "queue", true
						}
						r, _ := scope.NewRegistry(identity, &admissionDenial{})
						return r.Bind(c, scope.ScopeRef{TenantID: "queue", Kind: "tenant", ID: "queue"})
					}
				}
				return eventContext(t, "queue", scope.PrincipalRef{ID: "worker", Kind: "service"}), nil
			}
			calls := 0
			w, _ := inbox.NewWorker(store, manager(t, &postgres.Resource{Pool: worker}), group, bind, func(c context.Context, e distributed.Envelope) error { calls++; return insert(c, e.ID) }, 2, time.Hour)
			if ok, err := w.Process(context.Background()); ok || err == nil {
				t.Fatal(ok, err)
			}
			if ok, err := w.Process(context.Background()); err != nil || !ok || calls != 1 {
				t.Fatal("authorized event starved", ok, err, calls)
			}
			if _, err := admin.Exec(context.Background(), `UPDATE pangolin_events.inbox SET next_attempt_at=now()-interval '1 second' WHERE event_id=$1`, first.ID); err != nil {
				t.Fatal(err)
			}
			if ok, err := w.Process(context.Background()); ok || err == nil {
				t.Fatal(ok, err)
			}
			var state string
			var attempts int
			if err := admin.QueryRow(context.Background(), `SELECT state,attempts FROM pangolin_events.inbox WHERE event_id=$1`, first.ID).Scan(&state, &attempts); err != nil || state != "dead" || attempts != 2 {
				t.Fatal(err, state, attempts)
			}
			if count(t, admin, second.ID) != 1 {
				t.Fatal("effect missing")
			}
		})
	}
}
