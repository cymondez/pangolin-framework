---
title: SignalR 即時通知
---

使用 SignalR 讓已授權的使用者在業務操作提交後收到即時通知。部署可選 tenant-only 或 organization-enabled，同一租戶可以有多個獨立的企業及組織訂閱。

## 啟用模組

生成 Bookstore 專案時加入 `--realtime`。

```text
pangolin new bookstore --template modular-monolith --realtime
pangolin new scopedstore --template modular-monolith --with organization --realtime
```

從生成專案的 `REALTIME.md` 設定 JetStream、憑證 introspection 與 TLS。API 提供 `/hub`。客戶端須使用 SignalR JSON protocol 與 WebSocket，並提供目前有效的 access token。

```javascript
import { HubConnectionBuilder, HttpTransportType } from "@microsoft/signalr"

const connection = new HubConnectionBuilder()
  .withUrl("/hub", {
    transport: HttpTransportType.WebSockets,
    accessTokenFactory: getAccessToken,
  })
  .withAutomaticReconnect()
  .build()

connection.on("BookNotification", ({ id, data }) => renderNotification(id, data))
await connection.start()
const owner = await connection.invoke("CurrentScope")
await connection.invoke("Subscribe", {
  tenant: owner.TenantID,
  kind: owner.Kind,
  id: owner.ID,
  channel: "books",
})
```

`getAccessToken` 和 `renderNotification` 由應用提供。重新連線後需再次提交訂閱，伺服器會重新檢查目前資格。要退訂時，以相同參數呼叫 `Unsubscribe`。

## 發布通知

應用模組宣告直接相依並注入 `realtime.Publisher`。在已授權的寫入 Command 內，以同一 owner 寫入業務資料及通知事件。Outbox worker 將事件送到即時通訊專用的 JetStream subject。只在已授權且正在連線的 session 上配送，client 可用穩定事件 ID 處理重複。需要確認業務成功時，請使用 Command 結果或操作查詢。

框架原始碼的 `examples/realtime/README.md` 提供不需要外部 IdP 的 loopback 範例。該範例使用 PostgreSQL、JetStream 與官方 client，可在兩個服務程序上驗證推播、範圍隔離與撤權。
