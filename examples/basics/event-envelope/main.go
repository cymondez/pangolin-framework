package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
)

func main() {
	payload, err := json.Marshal(map[string]string{"id": "book-1", "title": "Go in Action"})
	if err != nil {
		log.Fatal(err)
	}
	event := distributed.Envelope{
		ID: "event-1", Type: "bookstore.book_created", SchemaVersion: 1,
		OccurredAt: time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC),
		TenantID:   "tenant-a", ScopeKind: "tenant", ScopeID: "tenant-a",
		CorrelationID: "request-1", CausationID: "create-book-1", Producer: "bookstore",
		Payload: payload,
	}
	raw, err := distributed.Encode(event)
	if err != nil {
		log.Fatal(err)
	}
	restored, err := distributed.Decode(raw)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s / %s / schema %d\n", restored.ID, restored.Type, restored.SchemaVersion)
	fmt.Println(string(restored.Payload))
}
