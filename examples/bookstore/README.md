# Bookstore：M1 真實垂直範例

此範例展示 HTTP → Application → Required Tx → PostgreSQL／Outbox，再由獨立 Worker 經 JetStream → Inbox → Read Model。純 tenant-only，不建立假 Organization；完整組織範本由 M5 交付。

| 目錄 | 責任與說明 |
|---|---|
| [domain](domain/README.md) | Book 不變條件、Repository Port，無外部 Adapter import |
| [application](application/README.md) | Create／Get 編排與 UOW／事件 Port |
| [infrastructure](infrastructure/README.md) | pgx/sqlc Repository、RLS schema、Outbox／Inbox／Projection 薄片 |
| [bootstrap](bootstrap/README.md) | DI／Module／Config／HTTP 與 Worker 的程序組裝 |
| [HTTP Adapter](../../transport/http/README.md) | 路由、JWT／Scope、Problem Details 與提交後回應 |

可執行入口為 `go run ./cmd/bookstore-api`、`go run ./cmd/bookstore-worker`；完整環境、連線變數、固定工具、測試與停機步驟見 [M1 開發指南](../../docs/guides/m1-development.md)。

此範例只有 M1 所需薄流程，不代表完整 UOW、安全、通用事件 Adapter、CLI、冪等或 Organization 已交付。每個子模組的 README 列出其邊界及測試。
