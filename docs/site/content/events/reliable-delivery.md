---
title: 組裝可靠事件配送（Local Event、Outbox 與 Inbox）
description: 完整示範如何將 Local Event Bus、有界 Outbox／Inbox Store 與 PostgreSQL UOW 串接在一起，並深入理解租約 token、行鎖處理與死信人工重送。
---

> **你在架構中的位置**：`Application 交易內` **`[ Local Bus + Outbox Publisher ]`** `→ UOW Completion（同交易落盤）→` **`[ Outbox Worker (短交易認領 + 交易外送 Broker) ]`** `→` **`[ Inbox Receiver (驗簽落盤去重) + Inbox Worker (同交易行鎖處理) ]`**
>
> **前置條件**：先閱讀[事件格式範例](integration-events.mdx)；本頁適用於你的 PostgreSQL／NATS 應用，需備妥可信 Scope、部署好的 `events/postgres.Migrations`（含容量遷移）與分權的 `appPool`／`queuePool`。

## 為什麼需要「兩個 DB Pool」與「把 Bus/Publisher 接上 UOW Resource」？

剛接觸 panGOlin 事件組裝的新人常有兩個疑問：
1. **為什麼不能只建一個 `outbox.Publisher` 直接呼叫，還非得包成 `eventpg.Resource` 交給 `uow.NewManager`？**
   因為只有把 `Local Bus` 與 `Outbox Publisher` 接進 UOW Resource，框架才能在**同一個交易完成階段**執行「Flush → 排空 Local Events → 驗證並同交易寫入 Outbox → 唯一一次 Commit」。沒有這條接線時呼叫 `publisher.Publish`，框架會直接拒絕，絕不退化成不可靠的直接送 Broker。
2. **為什麼要分開 `appPool` 與 `queuePool`？**
   - **`appPool`（業務最小權限帳號）**：受完整 owner RLS 保護，執行一般業務交易與同交易寫入 Outbox。
   - **`queuePool`（佇列維運帳號）**：負責背景 Outbox 認領、Inbox 接收、重試與容量管理（**絕不交給一般業務 Repository 使用**）。兩者指向同一業務資料庫，不承諾跨 DB 原子性。

---

## 1. 完整組裝檔案：`setup/events.go`

以下完整檔案可直接放進你的 Go module（執行 `go build ./...` 即可驗證組裝）：

```go
package setup

import (
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/events/local"
	"github.com/cymondez/pangolin-framework/events/outbox"
	eventpg "github.com/cymondez/pangolin-framework/events/postgres"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EventServices 將 PostgreSQL UOW、本機事件匯流排（Local Bus）與有界 Outbox/Inbox Store 接成一體。
// 呼叫端需先部署 events/postgres.Migrations 與 CapacityMigrations，並於 registry 註冊所有事件 Schema。
func EventServices(
	appPool, queuePool *pgxpool.Pool,
	registry *distributed.Registry,
	bus *local.Bus,
	retry outbox.Policy,
	capacity eventpg.Capacity,
) (uow.UnitOfWorkManager, *outbox.Publisher, *eventpg.Store, error) {
	// 步驟 1：建立受容量上限（Bounded Capacity）保護的 Outbox/Inbox Store。
	// 當積壓達上限時會拒絕新寫入（回 429）並使業務交易回滾，防止資料庫被無界佇列撐爆。
	store, err := eventpg.NewBoundedStore(queuePool, retry, capacity)
	if err != nil {
		return nil, nil, nil, err
	}
	// 步驟 2：建立 Outbox Publisher，綁定事件 Schema Registry 與 Store。
	publisher, err := outbox.NewPublisher(registry, store)
	if err != nil {
		return nil, nil, nil, err
	}
	// 步驟 3：用 eventpg.Resource 包住基礎 postgres.Resource，
	// 把 Local Bus 與 Outbox Publisher 正式接進 UOW 的完成階段（Completion）。
	resource := &eventpg.Resource{
		Base:      &postgres.Resource{Pool: appPool},
		Local:     bus,
		Publisher: publisher,
	}
	// 步驟 4：建立整合後的 UOW Manager，供 Application Service 使用。
	manager, err := uow.NewManager(resource, uow.DefaultOptions{
		DefaultTimeout:      5 * time.Second,
		CleanupTimeout:      time.Second,
		HookTimeout:         time.Second,
		MaxRequiresNewDepth: 2,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return manager, publisher, store, nil
}
```

### 參數設定檢查重點
- **`capacity`（有界背壓）**：Outbox 與 Inbox 上限皆須為正整數，部署時須一併套用 `eventpg.CapacityMigrations`（子目錄 `capacity_migrations`）。佇列滿額時回 429 並回滾該筆業務交易。
- **`registry`（事件契約註冊）**：須明示註冊所有 `distributed.Schema{Type, Version, Required, Validate}`；已發布的 `(Type, Version)` 絕不事後改寫。
- **`retry`（重試政策）**：`Lease`、`SendTimeout`、`RetryInitial`、`RetryMax`、`MaxAttempts` 皆須為正值，`JitterFraction` 介於 `0–1`。
- **業務內呼叫方式**：在可寫 UOW 回呼中先寫業務資料，再呼叫 `publisher.Publish(workCtx, envelope)`。`envelope` 的 Tenant 與完整 Scope 必須等於可信 binding，否則直接拒絕並標記交易 rollback-only。

---

## 2. 本機事件（Local Event）：同進程、同交易的模組協作

適合「**必須與主業務同交易成功或一起回滾**」的同進程後續動作（例如訂單建立時同步更新同單體內的統計彙總）：

- 以 `local.NewBus(Limits, handlers...)` 封存註冊；每個 Handler 綁定確切的 Go 事件型別，`Priority` 數值較小者先執行（同值按註冊順序）。
- 在業務回呼內呼叫 `bus.Enqueue(workCtx, evt)` 排隊；**`SaveChanges` 不會派送 Local Events**，只有進到 UOW 完成階段時才會依 FIFO 迭代排空並 flush。
- 超過 `MaxEventsPerUOW`／`MaxDispatchDepth` 或任一 Handler 回傳錯誤，都會使整筆交易失敗回滾。
- **❌ 禁止在 Local Event Handler 裡執行無法回滾的外部副作用**（例如直接發真實 Email 或打第三方付款 API）——那些請改發 Outbox Integration Event！

---

## 3. Outbox Worker 與 Inbox Consumer：為什麼兩者的交易設計「完全相反」？

這是分散式系統最關鍵的架構觀念之一，請務必分清楚：

```text
[ Outbox Worker（發送端）：短交易認領 ──▶ 放鎖、交易外送 Broker ──▶ 驗 lease_token 更新 sent ]
  理由：絕不能把資料庫交易與行鎖長時間掛在網路送 NATS 的 I/O 上！

[ Inbox Consumer（接收端）：Receiver 驗簽落盤後 ACK ──▶ Worker 在「同一筆業務交易內持有行鎖」處理業務 + 標 processed ]
  理由：業務資料修改與「標記此事件已處理」必須原子一致，否則處理完卻沒標記會重複扣款！
```

### Outbox Worker 重點
- `outbox.NewWorker(store, broker, retry, workerName, authorize)`：每次認領都會產生唯一 **`lease_token`**，並在**交易外**送往 Broker；只有 Broker 確認持久化與路由成功後，才用同一個 `lease_token` 更新為 `sent`。萬一某個 Worker 逾時卡住，它的舊 token 無法覆蓋已被其他 Worker 接手的新租約。
- NATS Stream 必須啟用 `FileStorage`，並確保 `subject`／`stream`／`durable consumer` 一致。停機時先停止認領、Drain，最後才關閉 Pool 與 NATS 連線。

### Inbox Receiver & Worker 重點
- **Receiver（接收去重）**：`inbox.NewReceiver(store, registry, group, verify, alert)` 先驗證完整 Envelope 與可信 producer，持久化寫入 Inbox 表後才呼叫 ACK。`verify` 必須依據 Broker ACL／session／route 等可信資訊判斷，**不可照抄 `Envelope.Producer`**。同一 `(group, event_id)` 自動去重；若同 ID 卻內容不同則隔離並觸發 `alert`。
- **Worker（同交易處理）**：`inbox.NewWorker(store, manager, group, bind, handler, maxAttempts, retry)` 每次執行都透過 `bind` 建立全新服務 context 並重新驗證當前 Scope（不重用已結束的 HTTP context）。授權拒絕或已確認回滾會做有界延期／隔離，防止一筆毒藥訊息卡死整個佇列；若發生 `Commit Unknown` 則不貿然宣稱失敗或重設狀態。
- 事件與背景工作可使用 `distributed.Execution.Run` 統一重綁服務身分與 Scope，並在新根 UOW 准入後重驗 action（使用者代理型 Job 另驗使用者目前資格）。

---

## 4. At-Least-Once 配送、死信重送與保留策略

1. **外部副作用的冪等責任**：Outbox／Inbox 提供的是 **at-least-once（至少一次）** 配送保證，不宣稱端到端 exactly-once。若 Consumer 要呼叫外部金流或寄信，請使用 `inbox.ExternalKey(group, event, operation)` 產生穩定外部冪等鍵搭配支援冪等的 Provider，或再寫一次 Outbox 派送。
2. **人工重送死信（ReplayDead）**：透過 `eventpg.NewMaintenance(...).ReplayDead` 走獨立維運 UOW（使用 `queuePool`），須具可信 owner、獨立 Permission 與同交易 Audit；重送會保留原 `EventID`、版本與完整 Envelope，已 `processed` 的事件不可重設。
3. **清理與保留視窗（Purge）**：`Purge` 只清理超過保留期的 `sent`／`processed` 紀錄，`pending`／`dead` 一律保留。請確保 **Inbox retention 長於「Broker 保留期 + 人工重送視窗」**，否則過早清掉 Inbox 紀錄可能讓重送訊息被誤當成新事件。

## 下一步

- 還沒看過 Command 重複提交防呆？前往 **[Command 冪等與結果對帳](../data/idempotency.md)**。
- 需要把後端提交完成的事件即時推播給前端瀏覽器？閱讀 **[SignalR 即時通知](../platform/realtime.md)**。
- 需要排程與非事件驅動的背景任務？閱讀 **[持久化背景工作](../platform/jobs.md)**。
