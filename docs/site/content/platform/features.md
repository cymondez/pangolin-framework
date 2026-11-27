---
title: Features 與配額（Feature Flags & Quotas）
description: 在 Permission 授權通過的前提下，管理 Boolean 功能開關、Quota 配額與有序 Enum 方案等級，並用 Ceiling 確保上層限制不被覆寫突破。
---

> **你在架構中的位置**：`已通過 Permission 檢查 →` **`[ platform/features（判斷客戶方案是否啟用功能／配額上限）]`**
>
> **前置建議**：先跑通[分層設定完整範例](settings.mdx)（`go run ./platform-services`），看懂範例中的 `books.limit` 與 `books.edit` 輸出。

## 為什麼需要把 Feature 與 Permission 嚴格分開？

在多租戶產品開發中，最常被混在一起的兩個概念是：
- **Permission（權限）**：決定「**這個使用者**有沒有資格執行這個動作」（安全邊界）。
- **Feature（產品功能／方案配額）**：決定「**這個客戶購買的方案**開沒開這項功能、最多能建幾筆」（商業邊界）。

panGOlin 在 `platform/features` 強制執行兩項保護：
1. **先驗 Permission，失敗根本不讀 Settings**：如果使用者缺少對應 Permission（或權威來源故障），`Evaluate` 直接拒絕，絕不會因為 Feature 設為 `true` 就幫使用者放行。
2. **Feature 不反向影響普通 Permission**：某個功能開關關閉，只代表該 Feature 不可用，不會去竄改使用者原本的 Permission 判定。

---

## 1. 定義與評估 Features

使用 `features.New(settingsResolver, authorizer, definitions)` 註冊功能清單。每個 `features.Definition` 需明示 `Setting`（對應的設定鍵）、`Permission`（前置必備權限）、`Kind`（型別），並可選擇設定 `ScopeKinds` 限制允許的 Scope 種類（留空表示接受任何已授權 Scope）。

支援三種嚴格強型別（型別不符或未知功能一律回 `FEATURE_INVALID`）：

| `Kind` | 合法值 | 直接拒絕的非法值 | 讀取結果欄位 |
|---|---|---|---|
| `features.Boolean` | `true`／`false` | `null`、數字、字串 | `val.Boolean` |
| `features.Quota` | 非負整數（`>= 0`） | 負數、小數、`null` | `val.Quota` |
| `features.Enum` | `Definition.Options` 內列舉的字串 | 不在選項清單內的值 | `val.Enum` |

---

## 2. 一般覆寫 vs 上限收斂（`Ceiling: true`）

這是 SaaS 配額系統最容易寫錯的地方！舉個例子：
- **情境 A（一般偏好開關，例如「是否顯示提示」）**：公司（Tenant）預設 `false`，員工自己改 `true` → 應該採用員工最優先的值（`Ceiling: false`，預設）。
- **情境 B（安全或付費配額上限，例如「最多建幾本書」）**：公司買的方案只允許最多 `10` 本（Tenant = `10`），如果員工在 User 層自己設了 `50`，**絕不能讓員工覆寫突破公司的方案上限**！這時就必須明示 **`Ceiling: true`**。

啟用 `Ceiling: true` 後，引擎會把所有候選層級（**包含 `Default`**）一起納入最嚴格收斂：

| 型別 | `Ceiling: true` 的收斂規則 | 實務提醒 |
|---|---|---|
| **Boolean** | 所有候選層**全部為 `true`** 才啟用（任一上層為 `false` 即關閉） | `Default` 須設為 `true`（允許最高上限），否則會不小心把全部子層關死！ |
| **Quota** | 取所有候選層的**最小值**（如 Tenant=10、User=50 → 有效配額 `10`） | `Default` 須設為產品允許的最大上限值。 |
| **Enum** | 依 `Options` 陣列由低到高的順序，取**最低等級**（如 `["basic", "pro"]` 中，父層 `basic` 會限制子層的 `pro`） | `Options` 是有序的等級階梯，不是任意字串交集。 |

---

## 3. 生命週期與責任歸屬

`features` 服務本身沒有背景 goroutine 或需關閉的資源，持久化完全交給底層的 Settings Provider。

## 下一步

前往 **[Scoped Memory Cache](cache.md)**，學習如何讓業務資料快取也享有同樣的完整 Scope 隔離與併發防擊穿（Singleflight）保護。
