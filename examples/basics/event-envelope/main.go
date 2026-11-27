package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
)

func main() {
	// 步驟 1：將領域事件內容序列化為 JSON Payload（不含敏感憑證或內部 DB 細節）。
	payload, err := json.Marshal(map[string]string{"id": "book-1", "title": "Go in Action"})
	if err != nil {
		log.Fatal(err)
	}

	// 步驟 2：建立版本化的標準事件信封（Envelope）。
	// 正式應用中，TenantID / ScopeKind / ScopeID 必須從受信任的 Scope context 填入，
	// 絕不可照抄未經授權的外部請求參數；重送時須保留同一個 ID 供接收端 Inbox 去重。
	event := distributed.Envelope{
		ID:            "event-1",
		Type:          "bookstore.book_created",
		SchemaVersion: 1,
		OccurredAt:    time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC),
		TenantID:      "tenant-a",
		ScopeKind:     "tenant",
		ScopeID:       "tenant-a",
		CorrelationID: "request-1",
		CausationID:   "create-book-1",
		Producer:      "bookstore",
		Payload:       payload,
	}

	// 步驟 3：編碼為可透過 Outbox / NATS 傳送的位元組，再解碼還原並驗證欄位完整性。
	raw, err := distributed.Encode(event)
	if err != nil {
		log.Fatal(err)
	}
	restored, err := distributed.Decode(raw)
	if err != nil {
		log.Fatal(err)
	}

	// 步驟 4：輸出還原後的事件識別、型別、Schema 版本與業務 Payload。
	fmt.Printf("%s / %s / schema %d\n", restored.ID, restored.Type, restored.SchemaVersion)
	fmt.Println(string(restored.Payload))
}
