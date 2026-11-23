# Transactional Outbox

`NewPublisher(registry, store)` 配合 `events/postgres.Resource.Publisher`，在可寫根 UOW 內 `Publish(ctx, distributed.Envelope)`。Envelope 必須與目前可信 owner 相等，符合已註冊 type／schema；入列即複製完整 wire bytes，完成階段同交易 Flush。缺 UOW／唯讀／未組裝 Outbox 一律拒絕，不退回直接 Broker Send。

`NewWorker(store, broker, policy, workerName, authorize)` 以短交易認領，每次唯一 token；交易外 Send，持久化與路由確認後才標 sent。成功／失敗都經 token fencing，舊 Worker 不得覆蓋新認領。Policy 明示 lease、send timeout、退避、jitter 與最大次數，失敗進 pending 或 dead。authorize 每次驗證可信 producer／目前服務資格，不能信任 Envelope 自稱的身分。

Worker 不關閉注入的 Store／Broker；組裝擁有者在 Drain 後關閉。可靠性是 at-least-once：Broker 確認後程序中斷可重送。dead 人工重送使用 PostgreSQL Adapter 的 `NewMaintenance(...).ReplayDead`，要求重送授權與同交易 Audit；終態保留清理使用 `Retention`／`Purge`，均保留 Event ID／owner 與 active 工作。Policy 必須與 Store 啟動配置一致，不接受不同 Worker 默默變更最大次數。完整運維容量仍由應用組裝設定。`go test ./events/...`、`go test -tags=integration ./tests/m3`。

## 運維與追蹤整合

Publisher 在未指定 TraceParent 時保存目前 W3C traceparent。Worker 還原追蹤並保留 CorrelationID；呼叫端以 Telemetry.Context 注入觀測。容量組裝使用 events/postgres.NewBoundedStore；容量超限會使同一業務交易失敗，不能退化成直接送 Broker。
