# Book Infrastructure：Repository 與事件薄片

[repository.go](repository.go) 實作 Domain Repository 與可靠 Publisher。每次從 callback context 取得受控 PostgreSQL Executor，附加可信 tenant 條件；查無與 unique violation 轉成 Domain 錯誤。Publish 只接受合法完整 Envelope，核對目前 owner，與 Book 同交易寫 Outbox，不退化為直接 Send。

## Schema 與生成物

`sql/schema.sql` 是 M1 tenant-only fixture schema：books／book_views 強制 tenant RLS，Outbox／Inbox 有專用 queue 權限。表由 pangolin_owner 擁有；app／worker 都不是 owner、superuser 或 BYPASSRLS。`sql/namespace.sql` 只供 sqlc parser catalog，實際 namespace 由部署角色 fixture 建立。

變更查詢時修改 `sql/queries.sql`／schema，再以 repository root 的 `sqlc.yaml` 與固定 sqlc v1.31.1 執行 `sqlc generate`；不要手改 `internal/bookdb`。Gate 會再生成比對。生成檔逐檔列在 coverage exclusions，手寫 Repository／wrapper 仍計入分母。

M1 integration harness 首次建立 schema；API 只查 schema 相容性。正式 Migration Runner、全域鎖與升級驗收留在 M3，不能將這個 fixture bootstrap 當成正式部署工具。

## Outbox、Inbox 與 Projection

[replication.go](replication.go) 的 Replicator 是本範例薄片，Pool 僅供受信 Worker 的 queue 操作，不提供給業務 Repository。Dispatch 以短交易 `SKIP LOCKED` 認領，產生 lease token，交易外發送；成功後以同 token 條件標 sent。M1 lease=2s、最多 8 次認領、Send timeout=5s；完整續租、重試／dead-letter／容量配置仍由 M3／M4 交付。

Receive 驗證服務的 tenant grant、Kind／ID、producer／type／version，持久化完整 Envelope；同 consumer group／event ID 同內容可重複，不同內容拒絕。呼叫端在持久化成功後 ACK。Project 在同一交易持有 Inbox row lock、安裝 tenant RLS、寫 Read Model 並標 processed。Worker 不能沿用 HTTP 使用者的舊 context。

`Replicator.Tenants` 是可信啟動設定的 M1 服務 allowlist；不是可變 Scope Provider／membership admission 的完整實作。一般 Outbox／Inbox Store 抽象與全部可靠性矩陣留待 M3。

驗證：依 [M1 指南](../../../docs/guides/m1-development.md) 執行真實 Integration／E2E，包含第二次寫入故障回滾、RLS、非法 Broker 目的地、Inbox 衝突及 Broker ACK 後 crash／重啟。
