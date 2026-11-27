# ADR05 — 單一 NATS JetStream Adapter

狀態：選型採用；可靠 Adapter 未實作。對應 ENV02、EVT-001–009。

首版唯一 Broker 選 NATS Server 2.15.3、JetStream file storage、durable pull consumer、explicit ACK。與 RabbitMQ 相較，本版先維護一種拓樸及可靠確認契約；不增加第二個 Adapter。server／image 的版本與 digest 固定於 Compose。

Outbox 的 Send 成功需要有效 JetStream publish acknowledgement，確認 stream／sequence 符合設定。沒有匹配 stream、被拒絕、timeout 或結果未知不可標 sent。Core NATS publish／flush 不是可靠確認。Inbox 先落地後 ACK，Worker 在同一業務交易持有 Inbox 行鎖，不能套用 Outbox 放鎖後發送流程。

本機 fixture 限定 256 MiB file store、16 MiB memory store、1 MiB payload、64 connections；生產環境需另定容量與 credentials／subject ACL。Compose 僅綁定 loopback，是 disposable 測試拓樸，不提供 HA 保證。M0 smoke 只證明 file stream publish／consume 與故障開關；崩潰重送、舊 lease、Inbox 併發由後續正式案例驗收。

依據：[NATS 發行資訊](https://docs.nats.io/release-notes/)、[JetStream consumers](https://docs.nats.io/learn/jetstream/pull-consumers)。
