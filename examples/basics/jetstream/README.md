# Bookstore 事件串流初始化

設定 PANGOLIN_NATS_URL 指向自己隔離的 NATS 後，從 `examples/basics` 執行 `go run ./jetstream -provision`。明確建立 BOOKSTORE_DEMO stream／bookstore.demo.book_created subject，輸出 ready 後結束。

這是 Bookstore 使用教學的明確部署步驟，需真實 JetStream；不會在 API 啟動時自動執行。Worker 以相同 PANGOLIN_NATS_STREAM／PANGOLIN_NATS_SUBJECT 接入。未提供 -provision 或 URL 時拒絕執行。既有 stream 的不相容配置會由 Adapter 回報錯誤。
