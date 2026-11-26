# SignalR Adapter

`New(gateway, options)` 回傳同時處理 Hub 路徑與 `/negotiate` 的 HTTP handler。以 `runtime.Participant` 接到 HTTP 生命週期，並讓 Quiesce 先於 listener Drain。反向代理需支援 WebSocket upgrade 並將同一次協商與升級送到同一節點。

只宣告 `WebSockets/Text` 和協商 v1。支援 JSON Hub Protocol v1、Invocation、Completion、server push、Ping、Close 與 cancellation。重新連線重新協商並訂閱。未實作其他 transport、MessagePack、streaming 或 stateful reconnect。

瀏覽器的 `accessTokenFactory` 在 WebSocket 升級使用 query credential，正式環境需使用 TLS 並從 proxy 日誌中移除 query。跨來源部署設定確切 `Origins`。連線、協商表、payload、讀寫、握手和心跳期限均須給定。握手與 transport 失敗釋放 session。Hub 回覆維持公開錯誤分類，詳細錯誤不送給 client。

官方 JavaScript client 驗證見 [Root 範例](../../../examples/realtime/README.md)。
