# 即時通訊

`realtime.Module` 是選用的 Root 模組。透過建構子注入應用部署的憑證權威、Tenant resolver、中立 Scope registry 和既有 Outbox publisher。模組輸出應用層 `Publisher` 與入口層 `Gateway`。可信組裝端可從 `StartIngress` 連接 Broker，將保留的 Hub 交給 receiver。

- 應用明確註冊 `Method` 及 `Channel`。方法透過自己的有界 context 呼叫 Application Port。資料使用既有 Command／Query 政策。連線不持有 Operation DI scope、UOW 或 SQL。
- 訂閱及配送使用完整 ScopeRef 和頻道，使用者配送另限制 UserID。Subscriber 依目前的權威資格及接收政策通過最終寫入 admission。權威故障拒絕當次操作，資格撤銷清除失效訂閱。
- `Publisher.Publish(ctx, destination, body)` 需要目前已授權的可寫 UOW。將 `realtime.Schema()` 加到該 UOW 使用的 Outbox serializer registry。返回的 ID 識別已暫存的事件。用 Command 的結果判定提交。
- 每連線佇列、訂閱和工作皆有有限容量。佇列滿載時關閉該連線。Runtime participant 先 Quiesce 再 Drain，等待已 admission 工作，隨後關閉連線。

連線以 `Authenticator` 重新檢查現行憑證及帳號。部署需接上能檢測撤銷的來源。發布政策亦應依業務內容確認目的 owner。本模組沒有全域 JoinGroup、跨租戶 Publish、離線投遞或 client ACK。

用 `go test ./modules/realtime/...` 驗證套件。publish／JetStream 案例（真實 PostgreSQL／NATS）由 `PANGOLIN_REALTIME_APP_DSN`／`PANGOLIN_REALTIME_NATS_URL` 啟用，缺變數時 skip；引用覆蓋值必須連同前置條件。以 [Root 範例](../../examples/realtime/README.md) 執行官方 client、Outbox 和兩節點實測；本模組的驗證範圍與使用邊界見 [SignalR 現況報告](../../docs/evidence/SignalR-status.md)，正式 M7 同版 Gate 驗收證據見 [M7 驗收紀錄](../../docs/evidence/M7.md)。
