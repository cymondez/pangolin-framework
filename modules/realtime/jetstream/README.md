# Realtime JetStream

部署先建立 limits-retention file stream 和專用 literal subject。授權發布角色只寫入對應 subject，節點角色只讀取授權 stream 並管理自身 consumer。`Route` 按 envelope type 將即時事件送到專用 Broker publisher，同時保留既有 Outbox 路由。`Start` 供每個服務程序取得獨立 DeliverNew consumer，並於停機時刪除該 consumer。

Receiver 驗證路由和事件契約，對有效事件保有有界的節點內穩定 ID 去重。節點完成有界本地交接後 ACK。權威與接收 policy 仍由連線在寫入前檢查。失敗重送可能產生重複，client 可用通知 ID 冪等。Broker 的 ACK、節點處置及 websocket write 均不表示 client 已收件。

Runtime 先呼叫 `Quiesce`，再等待 `Drain`。`Close` 回收 subscription。Broker 連線仍由其擁有者關閉。真實服務驗證見 [Root 範例](../../../examples/realtime/README.md)。
