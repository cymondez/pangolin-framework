# Bookstore Bootstrap：API／Worker 組裝

`Run(ctx, mode, args, out, decorate)` 以 Kernel Module／DI／Config／Participant 組裝獨立 `api` 或 `worker` 程序。實作見 [run.go](run.go)。可執行入口位於 `examples/bookstore/cmd/api`、`examples/bookstore/cmd/worker`；OS signal 或 `-run-for` 到期會走有界停機。

| 設定 | 用途 |
|---|---|
| PANGOLIN_DSN | 必填，API 使用 app role，Worker 使用 worker role；Pool 上限 4 |
| PANGOLIN_ISSUER | API 必填，可信 JWT issuer |
| PANGOLIN_JWKS_URL | API 公鑰來源，預設 issuer 下 `/jwks.json` |
| PANGOLIN_NATS_URL | Worker 必填，可信 Broker URL |
| PANGOLIN_WORKER_TENANTS | Worker 必填，可信服務 tenant allowlist，以逗號分隔 |
| PANGOLIN_NATS_STREAM／PANGOLIN_NATS_SUBJECT | 預設 PANGOLIN_M1／pangolin.m1.book_created |

Secrets 只從環境注入，不提供 DSN flag。`-listen` 預設 `127.0.0.1:18081`；`-run-for 10s` 是可選的有界 demo lifetime，負值拒絕。JWT audience 固定為 pangolin-bookstore；UOW 為 5s／2s cleanup／1s hook／depth 4。這是 M1 範例組裝，完整 CLI／Profile 生成器在 M5。

DB／Broker 都是 DI Singleton，Cleanup 是唯一關閉 owner。API Start 只檢查 Book schema，設定有界 HTTP timeout；HTTP Participant 先停止 admission，再 Shutdown／Drain。Worker Participant 先停止下一次認領，等待已接入工作，期限到才取消；最後反序 Stop 並由 Container 關閉依賴。

Worker 依序 Dispatch、接收並持久化後 ACK、Project；每次操作有 deadline，接收等待為短輪詢。故障回傳錯誤並退出，持久化狀態供重啟復原。`decorate` 只供 testkit 注入 Broker ACK 後 crash；正式兩個 main 都傳 nil。

啟動環境、schema／topology fixture 與驗證步驟見 [M1 指南](../../../docs/guides/m1-development.md)。驗證：`go test ./examples/bookstore/bootstrap` 加真實分程序 E2E；無設定、非法參數或相依不可用時不報 Ready。
