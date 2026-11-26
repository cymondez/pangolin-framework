# NATS：JetStream 可靠傳輸

`ConnectLive` 與 `SubscribeLive` 提供即時通訊 fan-out。每次訂閱建立一個獨立的 DeliverNew consumer，使用 literal subject 和 limits-retention file stream。節點離開時由 subscription 刪除自己的 consumer，broker 連線仍由組裝端關閉。原有的 `Connect`、`Subscribe` 繼續用於 Outbox／Inbox 工作分攤。

此套件將 [BrokerPublisher](../distributed/README.md) 對接真實 NATS JetStream。實作見 [broker.go](broker.go)，server／image digest 固定於部署配置；client 固定於 go.mod／go.sum。

```go
client, err := nats.Connect(url, streamName, subject)
// 檢查 err；連線擁有者在結束時 client.Close()。
err = client.Send(ctx, envelope)
subscription, err := client.Subscribe("bookstore")
delivery, err := subscription.Next(ctx)
// 先完整驗證並持久化 delivery.Envelope，成功後才 delivery.Ack(ctx)。
```

`Provision(ctx)` 是明確的部署／fixture 動作，建立 file stream（64 MiB、1 MiB message、168h retention）；API 啟動不執行它。Send 先確認目標 stream 採 FileStorage，再使用 envelope ID 作 Msg-Id，要求指定 stream 的 publish ACK 與非零 sequence。MemoryStorage、無匹配 stream、expected stream 不符、Broker 拒絕、timeout 或 ACK 無效都回傳錯誤。Core NATS publish／flush 不作為可靠成功證據。

Subscribe 拒絕空 durable 名稱，使用 durable pull consumer、explicit ACK、5s AckWait 與最多 8 次 Broker delivery。Next 只返回通過 codec 的完整事件。ACK 必須在 Inbox 已持久化或確認同內容重複後執行，不能在 handler 執行前先 ACK 未保存資料。資料處理的去重／原子性由 Inbox Store 負責；JetStream 的 Msg-Id 去重窗口不能取代它。

Connect 不自動重連；故障交由連線擁有者重建連線與持久化佇列復原。URL 使用部署端提供的 Broker 憑證，subject／stream 由組裝明示；部署必須限制各 producer 可發布的 subject，Receiver.Verify 依已驗證連線／路由辨識來源，不能以 Envelope 的 producer 欄位自我授權。此 Adapter 不自行管理 Broker 帳號、ACL 或 Secrets。

Outbox 的租約、退避及 dead-letter 由 [Outbox](../outbox/README.md) 與 [PostgreSQL Store](../postgres/README.md) 管理；同資料庫副作用由 [Inbox](../inbox/README.md) 保護。此路徑提供至少一次配送，外部副作用仍需接收端冪等鍵。長期容量與運維告警策略依使用應用的組裝設定。

驗證：`go test ./events/nats` 檢查無效設定與 ACK guards；`go test -tags=integration ./tests/m3 -run TestA52Broker` 使用固定版本的真實 NATS，驗證 durable ACK、routing 拒絕、DiscardNew 拒絕、timeout、MemoryStorage 拒絕與 Broker 重啟後還原消息／durable consumer。重啟子情境必須透過開發指南的專用 fixture controller 提供 `PANGOLIN_M3_BROKER_RESTART_DIR`；缺少時驗收失敗，不跳過。測試為每次執行建立唯一 stream 並精確清除。
