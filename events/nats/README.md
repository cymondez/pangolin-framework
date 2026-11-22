# NATS：JetStream 可靠傳輸切片

此套件將 [BrokerPublisher](../distributed/README.md) 對接真實 NATS JetStream。實作見 [broker.go](broker.go)，server／image digest 固定於部署配置；client 固定於 go.mod／go.sum。

```go
client, err := nats.Connect(url, streamName, subject)
// 檢查 err；連線擁有者在結束時 client.Close()。
err = client.Send(ctx, envelope)
subscription, err := client.Subscribe("bookstore")
delivery, err := subscription.Next(ctx)
// 先完整驗證並持久化 delivery.Envelope，成功後才 delivery.Ack(ctx)。
```

`Provision(ctx)` 是明確的部署／fixture 動作，建立 file stream（64 MiB、1 MiB message、168h retention）；API 啟動不執行它。Send 使用 envelope ID 作 Msg-Id，並要求指定 stream 的 publish ACK 與非零 sequence；無匹配 stream、expected stream 不符、timeout 或 ACK 無效都回傳錯誤。Core NATS publish／flush 不作為可靠成功證據。

Subscribe 使用 durable pull consumer、explicit ACK、5s AckWait 與最多 8 次 Broker delivery。Next 只返回通過 codec 的完整事件。ACK 必須在 Inbox 已持久化或確認同內容重複後執行，不能在 handler 執行前先 ACK 未保存資料。資料處理的去重／原子性由 Inbox Store 負責；JetStream 的 Msg-Id 去重窗口不能取代它。

M1 Connect 不自動重連；故障交由程序退出／重啟與持久化佇列復原。完整 reconnect、背壓／租約／重試／dead-letter 與 Broker 故障矩陣仍按 M3／M4 交付，不宣稱端到端 exactly-once。

驗證：`go test ./events/nats`，以及 [M1 真實服務測試](../../docs/guides/m1-development.md) 的 routing 拒絕、非法 Envelope、publish 後崩潰與同 EventID 重送。
