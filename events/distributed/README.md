# Distributed Events：Envelope 與 wire codec

此套件定義 [IntegrationEventPublisher／BrokerPublisher Port](contracts.go)，以及 [Encode／Decode](codec.go)。它不自行開交易、不直接讀寫業務資料，也不把 Domain Event 自動當成公開事件。

Envelope 保存 event ID、type、schema version、occurred_at、Tenant／Scope、correlation／causation、producer 與 payload。wire 欄位固定為 `event_id`、`event_type`、`schema_version`、`occurred_at`、`tenant_id`、`scope_kind`、`scope_id`、`correlation_id`、`causation_id`、`producer`、`payload`；不能使用 Go 預設的大寫欄位編碼作為跨程序契約。

```go
raw, err := distributed.Encode(envelope)
// 保存完整 raw；重送必須保留同一份 Envelope 與 event ID。
restored, err := distributed.Decode(raw)
```

必要識別、版本與時間不可空，payload 必須為合法 JSON；完整 envelope 上限 1 MiB。驗證失敗回傳 error，不傳送或 ACK 非法資料。codec 不授予 Scope 權限，receiver 仍需用目前服務身分重新檢查 owner。

M1 Bookstore 的可靠 publisher 在同一業務交易內寫 Outbox，見 [Infrastructure](../../examples/bookstore/infrastructure/README.md)；Broker 傳輸見 [NATS](../nats/README.md)。一般 Serializer Registry／Upcaster 與完整 EVT-009 相容性矩陣由 M3 交付。

驗證：`go test ./events/distributed`，包含完整欄位 round-trip、固定 wire 命名、非法／缺漏欄位及大小上限。
