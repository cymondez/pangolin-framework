---
title: Command 冪等與結果對帳（Idempotency）
description: 透過資料庫唯一約束與同交易結果保存，讓付款、建單等關鍵寫入可安全重試，並在發生 UOW_COMMIT_UNKNOWN 時可靠對帳。
---

> **你在架構中的位置**：`HTTP 入口（帶 Idempotency-Key）→ 權限驗證 →` **`[ UOW 寫入交易：預留冪等鍵 + 業務寫入 + 保存保護後結果 ]`** `→ 同交易 Commit`
>
> **前置條件**：需備妥 PostgreSQL UOW、可信 Scope、當前 action 授權檢查，以及部署好 `persistence/idempotency.Migrations`。

## 為什麼需要 Command 冪等？

想像一個真實線上事故：客戶按下「確認付款／建立訂單」，你的後端已經在 PostgreSQL **成功 Commit** 了，但在回傳 HTTP 200 給前端的瞬間網路中斷（或 Load Balancer 逾時）。

這時候用戶端只看到「連線逾時」，如果它自動重試送出同一筆請求：
- **沒有冪等機制**：後端會再建一張一模一樣的訂單、扣兩次款！
- **只靠「先查再寫（Check-then-Act）」**：兩個重試請求同時抵達時，兩邊都查到「還沒處理」，最後依然雙重扣款。

panGOlin 的 `persistence/idempotency` 用**資料庫唯一約束 + 同交易保存結果**徹底解決這個問題：

```text
收到 Command (operation, key, body)
  │
  ▼
[1. 交易內先以 DB 唯一約束保留 key]（鍵包含 Tenant + Scope + Principal + operation + key）
  ├─ 全新 key ──▶ 執行你的業務 work(workCtx) ──▶ 將保護後的 JSON 結果與業務／Outbox「同交易 Commit」
  ├─ 相同 key 且 body 指紋完全相同：
  │     ├─ 已完成 ──▶ 重新驗證目前權限 ──▶ 直接重播原成功結果（replayed = true），不重跑業務！
  │     └─ 併發進行中 ──▶ 等候前一個持有者 Commit 或 Rollback
  └─ 相同 key 但 body 內容不同 ──▶ 拒絕並回傳 IDEMPOTENCY_CONFLICT（防止拿舊 key 塞新參數）
```

---

## 1. 組裝 `idempotency.Store` 與完整業務服務範例

### 組裝 Store 的必要條件
呼叫 `idempotency.New(cfg, authorize)` 時必須設定：
- `MaxKeyBytes`、`MaxBodyBytes`：限制鍵與請求體大小上限，防堵惡意塞爆儲存。
- `RetryWindow`、`Retention`：重試視窗與保留期限（**`Retention` 不得短於 `RetryWindow`**）。
- `Protect` / `Unprotect`：結果保護函式——若回應含敏感資料應接入部署的加密器；即使非敏感結果也須明示編碼策略。
- `authorize`：每次執行與查詢結果時重新核對當前操作權限（不能用舊快取繞過撤權）。

### 完整範例：`orders/service.go`

以下完整檔案可直接放入你自己專案的 `orders/service.go` 編譯使用。我們以 `application.CommandResult` 搭配 `CreateResult` 結構，確保**只有在交易確認 Commit 成功後**才把結果回傳給上層：

```go
package orders

import (
	"context"

	"github.com/cymondez/pangolin-framework/application"
	"github.com/cymondez/pangolin-framework/persistence/idempotency"
	"github.com/cymondez/pangolin-framework/persistence/uow"
)

// CreateResult 封裝冪等執行的回傳值，讓呼叫端知道結果是「首次建立」還是「安全重播」。
type CreateResult struct {
	Payload  []byte // 可安全重播的標準化 JSON 回應內容
	Replayed bool   // true 代表此 key 先前已成功，本次直接重播原結果、未重複執行業務
}

// Service 代表訂單應用服務，將 UOW Manager 與冪等 Store 透過建構子明確注入。
type Service struct {
	manager uow.UnitOfWorkManager
	keys    *idempotency.Store
}

func New(manager uow.UnitOfWorkManager, keys *idempotency.Store) *Service {
	return &Service{manager: manager, keys: keys}
}

// Create 以單一原子交易保護訂單建立流程：
// 1. application.CommandResult 開啟可寫交易（ReadWrite + Transactional）。
// 2. keys.Execute 在業務執行前先以 DB 唯一約束保留 key；
//    業務 work 只使用傳入的交易 context，並回傳可重播的 JSON payload。
// 3. 若 work 失敗，整筆交易 Rollback，不會留下卡死的 pending key。
func (s *Service) Create(
	ctx context.Context,
	key string,
	body []byte,
	work func(context.Context) ([]byte, error),
) (CreateResult, error) {
	return application.CommandResult(ctx, s.manager, func(workCtx context.Context) (CreateResult, error) {
		payload, replayed, err := s.keys.Execute(workCtx, "orders.create", key, body, work)
		if err != nil {
			return CreateResult{}, err
		}
		return CreateResult{Payload: payload, Replayed: replayed}, nil
	})
}
```

> 呼叫 `Service.Create` 前，入口仍須依[HTTP 與稽核](../security/http-and-audit.md)完成身分驗證與 Tenant／Scope 綁定。

---

## 2. 冪等鍵的範圍與指紋規則

1. **複合唯一鍵涵蓋完整身分邊界**：資料庫唯一鍵包含 **`(TenantID, ScopeKind, ScopeID, PrincipalKind, PrincipalID, operation, key)`**。不同租戶、不同 Scope 或不同使用者即使巧合用到相同字串 key，彼此完全隔離，絕不會看到對方的回應結果。
2. **請求體（`body`）必須是合法 JSON**：
   - JSON 物件的欄位順序不影響指紋計算，但數值保留字面格式（例如 `10` 與 `10.0` 不同）。請在第一次進入 `Execute` 前完成自家 DTO 的標準化。
   - 若同一個 key 第二次帶了不同內容，直接回傳 `IDEMPOTENCY_CONFLICT`。
3. **失敗自動標記 Rollback-only**：只要 `Execute` 任一步失敗，根交易就會被標成 rollback-only——即使外層捕捉錯誤，也無法把殘留的 pending key 提交進資料庫。

---

## 3. 收到 `UOW_COMMIT_UNKNOWN` 時如何對帳？

當寫入呼叫回傳 `uow.ErrCommitUnknown` 時，交易可能已成功也可能失敗。這時請**保留原來的 `key`**，另開一筆新的已授權**唯讀 UOW** 呼叫 `Store.Query(ctx, operation, key)` 對帳：

| 回傳狀態（`State`） | 代表意義 | 應用應如何處理 ✅ |
|---|---|---|
| `succeeded` | 原交易其實已經成功 Commit | `Payload` 就是解密／解碼後的原成功結果，安全回傳給用戶端，**不要重複執行**。 |
| `in_progress` | 原交易仍在執行或收尾中 | 短暫退避等候後，以相同 `key` 做有界重查。 |
| `unknown` | 資料庫查無紀錄 | **重要**：查無紀錄仍不能 100% 證明原操作絕未落盤（例如主從延遲或網路分區），應繼續按業務對帳流程核對，不可盲目當成未發生。 |
| `expired` | 已超過 `Retention` 保留期限 | 系統不再承諾用同一 key 防重，應停止自動重試並由人工或業務規則處理。 |

---

## 4. 安全與外部副作用提醒

- **重播也必須重驗目前權限**：`Execute` 重播與 `Query` 查詢都會重新呼叫 `authorize`。如果使用者提交成功後就被管理員撤權，已提交的資料不會被刪除，但該使用者無法再利用舊冪等鍵把結果偷讀出來。
- **外部金流／寄信仍需外部冪等鍵**：本機制保護的是你自己的業務資料庫與 Outbox。如果業務要呼叫第三方金流或寄信 API，請改採「同交易寫入 Outbox + 背景 Worker 搭配穩定外部冪等鍵」的兩階段模式，詳見[組裝可靠事件配送](../events/reliable-delivery.md)。

## 下一步

前往 **[部署資料庫遷移](migrations.md)**，學會用 Goose 與全域部署鎖安全套用包含 `idempotency.Migrations` 在內的所有 Schema。
