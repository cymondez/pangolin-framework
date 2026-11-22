# 事件編解碼

從 `examples/basics` 執行 `go run ./event-envelope`，輸出 `event-1 / bookstore.book_created / schema 1` 與書籍 JSON。無需 Broker。

本例只示範公開 Envelope／Encode／Decode，並未發送事件，也不建立租戶授權。事件的 ScopeRef 必須由實際可信操作取得；可靠保存與配送請看 Bookstore。
