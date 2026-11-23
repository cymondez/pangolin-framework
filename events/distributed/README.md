# Distributed Events：Envelope 與 wire codec

此套件定義 [IntegrationEventPublisher／BrokerPublisher Port](contracts.go)，以及 [Encode／Decode](codec.go)。它不自行開交易、不直接讀寫業務資料，也不把 Domain Event 自動當成公開事件。

Envelope 保存 event ID、type、schema version、occurred_at、Tenant／Scope、correlation／causation、producer 與 payload。wire 欄位固定為 `event_id`、`event_type`、`schema_version`、`occurred_at`、`tenant_id`、`scope_kind`、`scope_id`、`correlation_id`、`causation_id`、`producer`、`payload`；不能使用 Go 預設的大寫欄位編碼作為跨程序契約。

```go
raw, err := distributed.Encode(envelope)
// 保存完整 raw；重送必須保留同一份 Envelope 與 event ID。
restored, err := distributed.Decode(raw)
```

必要識別、版本與時間不可空，payload 必須為 JSON object；沒有上游原因的 causation_id 編碼為 null。完整 envelope 上限 1 MiB。驗證失敗回傳 error，不傳送或 ACK 非法資料。codec 不授予 Scope 權限，receiver 仍需用目前服務身分重新檢查 owner。

`NewRegistry(Schema{Type,Version,Required,Validate})` 封存明示的版本契約，拒絕重複註冊與未知版本。Required 保護既有欄位，Validate 可實作欄位型別／業務 payload 契約；新增 optional 欄位可向後相容。變更必需欄位或語意應新增版本，並繼續註冊仍有 consumer 使用的舊版本。`LegacyTenant` 只對明示的舊 tenant-only schema 將同時缺少的 scope_kind／scope_id 回填為 tenant／tenant_id，不猜測 Organization owner。

`Execution.Run` 是 Event／Job 共用的 Scope 執行契約：`Task` 只保存完整 ScopeRef、可信 producer 參照、Permission 及可選 delegated principal。執行時注入目前經驗證的服務情境與 IdentityReader、Resolver、TrustedProducer／Authorize；代理使用者的工作另以 Delegation 檢查目前資格。框架再進入新根可寫 UOW，Scope admission 會重新確認權威 revision。它不保存 HTTP context，也不取代 Job 的持久化／租約 Worker。

可靠 publisher 見 [Outbox](../outbox/README.md)，接收／處理見 [Inbox](../inbox/README.md)，完成階段組裝見 [PostgreSQL Adapter](../postgres/README.md)，Broker 傳輸見 [NATS](../nats/README.md)。舊 Bookstore 薄片仍供既有應用回歸；正式框架佇列使用獨立 pangolin_events schema。

驗證：`go test ./events/distributed`，包含完整欄位 round-trip、固定 wire 命名、非法／缺漏欄位及大小上限。

Execution 在入口及新根 admission 之後各自驗證目前 Permission 與代理使用者資格；權威 revision 更新不會重用 admission 前的 allow。已開始短操作的授權快照語意仍由 UOW／Provider 維持。
