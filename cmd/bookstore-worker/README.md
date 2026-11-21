# Bookstore Worker 程序

入口為 `go run ./cmd/bookstore-worker`。先設定 PANGOLIN_DSN（worker role）、PANGOLIN_NATS_URL 與 PANGOLIN_WORKER_TENANTS；JetStream topology 必須由明確的 fixture／部署步驟建立。可明示 PANGOLIN_NATS_STREAM／PANGOLIN_NATS_SUBJECT。

Ctrl+C／SIGTERM 或 `-run-for 10s` 觸發停止下一次認領與有界 Drain，最後才關閉 Broker／DB。錯誤非零退出，已持久化工作可由重啟程序恢復。詳細邊界見 [Infrastructure](../../examples/bookstore/infrastructure/README.md) 與 [bootstrap](../../examples/bookstore/bootstrap/README.md)。

驗證由 [M1 指南](../../docs/guides/m1-development.md) 的真實分程序 E2E 執行；testkit 的 crash-worker 僅是故障裝置，不替代此正式 Worker。
