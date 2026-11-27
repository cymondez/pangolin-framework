//go:build integration

package m2

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pangolin-framework/pangolin/persistence/postgres"
	transport "github.com/pangolin-framework/pangolin/transport/http"
)

func TestA49Stream(t *testing.T) {
	app := pool(t, "PANGOLIN_M1_APP_DSN")
	m := manager(t, app)
	next := make(chan struct{})
	idle := make(chan struct{})
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		ctx := binding(t)
		for i := 0; i < 2; i++ {
			var id uint64
			e := transport.StreamOperation(ctx, m, func(c context.Context) error {
				ex, err := postgres.Current(c, false)
				if err != nil {
					return err
				}
				return ex.QueryRow(c, "SELECT txid_current()").Scan(&id)
			})
			if e != nil {
				done <- e
				return
			}
			if _, e = fmt.Fprintf(w, "data: %d\n\n", id); e != nil {
				done <- e
				return
			}
			w.(http.Flusher).Flush()
			idle <- struct{}{}
			select {
			case <-next:
			case <-r.Context().Done():
				done <- r.Context().Err()
				return
			}
		}
		done <- nil
	}))
	defer server.Close()
	response, e := http.Get(server.URL)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	var first, second uint64
	for i := 0; i < 2; i++ {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		_, _ = reader.ReadString('\n')
		var id uint64
		if _, e = fmt.Sscanf(line, "data: %d", &id); e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			first = id
		} else {
			second = id
		}
		<-idle
		if app.Stat().AcquiredConns() != 0 {
			t.Fatal("SSE retained DB transaction", app.Stat())
		}
		next <- struct{}{}
	}
	if e := <-done; e != nil || first == second {
		t.Fatal(e, first, second)
	}
}
