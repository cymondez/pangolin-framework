# Bookstore：M1 真實垂直範例

此範例展示 HTTP → Application → Required Tx → PostgreSQL／Outbox，再由獨立 Worker 經 JetStream → Inbox → Read Model。純 tenant-only，不建立假 Organization；完整組織範本由 M5 交付。

| 目錄 | 責任與說明 |
|---|---|
| [domain](domain/README.md) | Book 不變條件、Repository Port，無外部 Adapter import |
| [application](application/README.md) | Create／Get 編排與 UOW／事件 Port |
| [infrastructure](infrastructure/README.md) | pgx/sqlc Repository、RLS schema、Outbox／Inbox／Projection 薄片 |
| [bootstrap](bootstrap/README.md) | DI／Module／Config／HTTP 與 Worker 的程序組裝 |
| [HTTP Adapter](transport/http/README.md) | 路由、JWT／Scope、Problem Details 與提交後回應 |
| [API 入口](cmd/api/README.md)、[Worker 入口](cmd/worker/README.md) | 各自獨立的程序與退出／停機方式 |
| [internal/testkit](internal/testkit/README.md) | 只供範例 E2E 使用的 crash／對帳程序 |

可執行入口為 `go run ./examples/bookstore/cmd/api`、`go run ./examples/bookstore/cmd/worker`；完整環境、連線變數、固定工具、測試與停機步驟見 [M1 開發指南](../../docs/guides/m1-development.md)。

此範例只有 M1 所需薄流程，不代表完整 UOW、安全、通用事件 Adapter、CLI、冪等或 Organization 已交付。每個子模組的 README 列出其邊界及測試。
