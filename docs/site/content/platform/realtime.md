---
title: SignalR 即時通知（Realtime）
description: 使用可選的 SignalR 模組與 NATS JetStream，讓已授權的使用者在業務交易確認提交後收到安全隔離的即時推播。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

> **你在架構中的位置**：`業務 Write Command（同交易寫入 Outbox）→` **`[ 獨立 Outbox Worker → NATS JetStream → 各節點 SignalR Receiver → /hub WebSocket Client ]`**
>
> **前置建議**：先了解[組裝可靠事件配送](../events/reliable-delivery.md)與[資源 Scope](../security/resource-scopes.md)。

## 為什麼 SignalR 推播也要走「同交易 Outbox + JetStream」？

如果直接在 HTTP Handler 裡寫完 DB 就立刻把訊息推給 WebSocket：
1. **跨節點收不到**：使用者連在 API 節點 A，但寫入請求打在 API 節點 B，單機記憶體推播完全送不到。
2. **幻覺通知**：DB 交易後來 Rollback 了，前端畫面卻已經彈出「訂單建立成功」通知。
3. **撤權洩漏**：使用者被停權或移出組織後，舊 WebSocket 連線如果沒做重驗與隔離，仍會持續偷聽到該組織推播。

panGOlin 的可選 `modules/realtime` 模組把即時通訊安全地接在既有的 UOW、Scope 與 Outbox 管線上，同時支援 `tenant-only` 與 `organization-enabled` 兩種部署模式（同一租戶下多個 Enterprise 與 Organization 訂閱彼此完全隔離）。

---

## 1. 透過 CLI 快速啟用 SignalR 模組

在建立新專案時加上 `--realtime` 旗標即可生成完整骨架（包含 `/hub` 端點與專屬說明文件 `REALTIME.md`，CLI 建置方式見[建立專案：`pangolin new`](../reference/cli-new.md)）：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# Tenant-only 模式 + SignalR 即時通知
& $pangolin new bookstore --template modular-monolith --realtime

# Organization-enabled 模式 + SignalR 即時通知
& $pangolin new scopedstore --template modular-monolith --with organization --realtime
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# Tenant-only 模式 + SignalR 即時通知
"$PANGOLIN" new bookstore --template modular-monolith --realtime

# Organization-enabled 模式 + SignalR 即時通知
"$PANGOLIN" new scopedstore --template modular-monolith --with organization --realtime
```

</TabItem>
</Tabs>

建立完成後，請依生成專案根目錄的 `REALTIME.md` 設定 JetStream、憑證 introspection 與 TLS。

---

## 2. 前端連線與訂閱範例（官方 `@microsoft/signalr`）

API 端點位於 `/hub`。客戶端必須使用 **SignalR JSON Protocol + WebSockets**，並透過 `accessTokenFactory` 提供當前有效的 access token：

```javascript title="前端連線、取得當前 Scope 並訂閱頻道"
import { HubConnectionBuilder, HttpTransportType } from "@microsoft/signalr";

// 步驟 1：建立強制 WebSocket 的 SignalR 連線，由應用提供最新有效 Token。
const connection = new HubConnectionBuilder()
  .withUrl("/hub", {
    transport: HttpTransportType.WebSockets,
    accessTokenFactory: getAccessToken,
  })
  .withAutomaticReconnect()
  .build();

// 步驟 2：註冊事件監聽器（客戶端應使用穩定的 id 做重複通知去重）。
connection.on("BookNotification", ({ id, data }) => renderNotification(id, data));

// 步驟 3：啟動連線並向伺服器查詢目前授權的 Scope，再顯式提交訂閱。
await connection.start();
const owner = await connection.invoke("CurrentScope");
await connection.invoke("Subscribe", {
  tenant: owner.TenantID,
  kind: owner.Kind,
  id: owner.ID,
  channel: "books",
});
```

### 客戶端實務注意事項
- `getAccessToken` 與 `renderNotification` 由你的前端應用自行實作。
- **斷線重連必須重新訂閱**：自動重連（`onreconnected`）後需再次呼叫 `Subscribe`，伺服器會重新檢查該使用者當前的租戶與組織資格。
- 退訂時，以相同參數呼叫 `connection.invoke("Unsubscribe", ...)`。

---

## 3. 後端如何發布即時通知？

1. 你的業務模組在 `DependsOn()` 宣告直接相依即時通訊模組，並透過建構子注入 `realtime.Publisher`。
2. 在已授權的寫入 Command（UOW 交易）內，以**同一個 owner Scope** 寫入業務資料並呼叫 `realtime.Publisher` 發布通知事件（與業務同交易寫入 Outbox）。
3. **只對在線且已授權的 Session 推播**：即時推播屬於「盡力通知（best-effort UI 更新）」，用來即時刷新畫面；客戶端可用穩定事件 ID 去重，若要**確切確認業務寫入成功**，請一律以 Command API 回應或查詢端點為準。

---

## 4. 部署關鍵提醒：務必啟動獨立 Outbox Worker！

> **⚠️ 新手最常踩的部署坑**：
> 透過 `--realtime` 生成的 API 程序會自動啟動各節點的 **Receiver** 並連上 JetStream 監聽推播，但 **API 本身不包含 Outbox Worker**！
>
> 你必須在部署環境中啟動一個獨立的 Outbox dispatch 程序（將 `jetstream.Route` 注入 `outbox.NewWorker`），事件才會從資料庫的 Outbox 表真正被送到 JetStream；否則通知只會停留在資料庫裡，前端完全收不到推播！（生成專案的 `REALTIME.md` 亦有相同組裝步驟說明。）

### 本機雙節點實測範例
框架原始碼的 `examples/realtime/README.md` 提供**不需要外部 IdP** 的本機 loopback 示範，使用 PostgreSQL、JetStream 與官方 SignalR client，讓你可以親手在兩個獨立服務程序上驗證跨節點推播、Scope 隔離與撤權斷線。

## 下一步

接下來進入平台能力章節：閱讀 **[分層設定與 Features](settings.mdx)**，學會處理多租戶／使用者的動態偏好、產品配額與快取隔離。
