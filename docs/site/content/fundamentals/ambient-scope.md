---
title: Ambient Scope（顯式環境範圍）
description: 理解 panGOlin 如何以 kernel/ambient 的不可變 Copy-on-Write 巢狀鏈取代 GLS，兼顧多執行緒平行隔離、顯式上下文傳遞與防偽造安全邊界。
---

> **你在架構中的位置**：`HTTP / Worker 入口 →` **`[ kernel/ambient：以 context.Context 承載不可變 Ambient Scope 鏈 ]`** `→ Identity / Tenant / ScopeBinding / UOW / Executor / Telemetry`
>
> **前置建議**：先閱讀[安排你的應用程式](../concepts/architecture.md)與[依賴注入](dependency-injection.mdx)。

## 為什麼需要 Ambient Scope？

在企業級模組化應用中，許多橫切狀態必須沿著一次請求或背景操作的呼叫鏈自然流過各層，例如：

- 已驗證的身分（`security/identity`）與租戶資格（`tenancy`）
- 不可變的資源所有權綁定（`security/scope` 的 `ScopeBinding`）
- 當前的工作單元視圖（`persistence/uow`）與受控資料庫執行器（`persistence/database`、`persistence/postgres`）
- 交易綁定的本機事件／Outbox 佇列（`events/local`、`events/outbox`）與追蹤識別（`observability`）

如果把這些狀態當成一般業務參數逐層手寫傳遞，每個 Domain／Application 介面都會塞滿 `*sql.Tx`、`TenantID`、`OutboxQueue` 等基礎設施雜訊；反之，如果為了省事放進全域變數或執行緒區域儲存，又會立刻引發跨請求汙染、測試失真與平行競態。

panGOlin 在 **`kernel/ambient`** 提供統一底層原語 **`ambient.Var[T]`**（僅依賴 Go 標準函式庫 `context`，零框架內部依賴），把所有橫切環境狀態收斂為一套一致、可驗證的不可變模型。

---

## 與執行期隱式傳遞模型（如 `AsyncLocal<T>`）的結構對照

在許多具備執行期環境傳遞機制的語言與框架中（例如以 `AsyncLocal<T>` 管理租戶、工作單元或使用者身分的設計），核心價值在於「每個非同步邏輯流保有獨立的 Copy-on-Write 狀態鏈，互不干擾」。`kernel/ambient` 的 `ambient.Var[T]` 在結構上具備相同的不可變隔離特性，同時堅持 Go 語言的顯式傳遞慣例：

| 設計面向 | 隱式執行流模型（如 `AsyncLocal<T>`） | panGOlin（`kernel/ambient` — `ambient.Var[T]`） |
|---|---|---|
| **傳遞載體** | 由語言執行期隨非同步流程隱式流動 | Go 標準 `context.Context` 顯式傳遞 |
| **巢狀覆寫模型** | 變更時複製執行環境（Copy-on-Write），離開範圍還原外層值 | 不可變單向鏈結 `frame[T]{value, present, outer, depth}`（Copy-on-Write），離開函式自然回到未變動的父 `ctx` |
| **平行分支隔離** | 多個非同步分支從同一父範圍各自開啟獨立子範圍，互不干擾父層 | 多個 goroutine 從同一父 `ctx` 同時呼叫 `With`／`Clear`／`RequiresNew`，**零共享可變狀態、零計數器**，互不鎖死 |
| **清除／脫離範圍** | 清空當前邏輯流槽位建立乾淨分支 | `v.Clear(ctx)` 插入已清除哨兵 frame（`Current=false`、`Depth=0`），父 `ctx` 絲毫不受影響 |
| **防偽造安全邊界** | 透過內部存取器限制寫入權 | 每個 `Var[T]` 持有獨立且非零大小的未匯出指標鍵；各套件將 `Var[T]` 保持 **package-private**，外部無法偽造 |

---

## 為什麼 panGOlin 堅決不採用 GLS（Goroutine-Local Storage）？

Go 語言刻意不提供類似 Java `ThreadLocal` 或 .NET `AsyncLocal` 的內建 GLS。坊間偶爾會用解析 `runtime.Stack` 取得 goroutine ID 再查全域 `sync.Map` 的方式模擬 GLS，但 panGOlin 明確禁止這類作法：

1. **脆弱且違反 Go 相容性承諾**：依賴未公開的 runtime 內部布局或堆疊文字格式，Go 版本升級即可失效，且每次查詢都伴隨鎖競爭或效能損耗。
2. **隱藏資料流與生命週期洩漏**：一旦開啟新 goroutine，隱式 GLS 不會自動繼承或隨取消訊號結束，極易發生「忘記清理導致記憶體洩漏」或「靜默讀到空租戶」。
3. **Go 的慣例就是顯式 `context.Context`**：`context.Context` 本身就結合了取消（Cancellation）、期限（Deadline）與不可變值鏈。將 Ambient Scope 建在 `context.Context` 上，讓每次權限、租戶與交易傳遞在程式碼審查與靜態分析中完全透明。

---

## 安全防偽造：Package-Private `ambient.Var[T]` 與封閉的 `ambient.Slot`

許多 Go 專案直接用 `context.WithValue(ctx, "tenant_id", ...)`，這會帶來兩個風險：**字串鍵碰撞**與**外部程式碼任意覆寫安全狀態**。

`kernel/ambient` 透過兩個機制徹底消除這兩類問題：

1. **獨立指標鍵 + 套件私有宣告**：每次呼叫 `ambient.New[T]()` 都會配置一個具非零大小的未匯出 `slotKey[T]` 指標（確保 Go runtime 不會合併零大小物件位址）。各擁有者套件把 `var bindingVar = ambient.New[ScopeBinding]()` 宣告為未匯出變數，只透過經過驗證的公開 API（例如 `scope.Registry.Bind`）寫入；任何外部程式碼或 DTO 都不可能繞過授權偽造值。
2. **封閉的 `ambient.Slot` 清除契約**：當生命週期邊界（例如 `uow.Detach` 或 `uow.RequiresNew`）需要清除交易相關的資源槽位（`database`／`postgres` Executor、Local／Outbox 佇列）時，相關資源套件會透過 `uow.AttachSlot(ctx, executorVar)` 登記自己的 `*ambient.Var[T]`。由於 `ambient.Slot` 介面包含未匯出標記方法，**只有真正的 `*ambient.Var[T]` 才能實作**，因此 `uow.Detach` 只能執行受控的 `Clear`，絕不會接受任意的 context 改寫回呼。

---

## `kernel/ambient` API 速查與範例

```go
package myfeature

import (
	"context"

	"github.com/cymondez/pangolin-framework/kernel/ambient"
)

// 宣告為 package-private，只讓本套件的受控 API 讀寫。
var correlationVar = ambient.New[string]()

func WithCorrelation(ctx context.Context, id string) context.Context {
	return correlationVar.With(ctx, id)
}

func CurrentCorrelation(ctx context.Context) (string, bool) {
	return correlationVar.Current(ctx)
}

func RunWithOverride(ctx context.Context, subID string) (int, error) {
	// ScopeResult 衍生新 ctx 執行回呼；因 ctx 不可變，函式返回或 panic 時外層自動保持原值。
	return ambient.ScopeResult(correlationVar, ctx, subID, func(inner context.Context) (int, error) {
		cur, _ := correlationVar.Current(inner) // subID
		outer, _ := correlationVar.Outer(inner) // 呼叫前的原本值（若存在）
		depth := correlationVar.Depth(inner)    // 巢狀深度
		_, _ = cur, outer
		return depth, nil
	})
}

func DetachCorrelation(ctx context.Context) context.Context {
	// Clear 建立已清除 frame（Current=false、Depth=0），不影響任何父 ctx。
	return correlationVar.Clear(ctx)
}
```

### API 行為重點

| 方法 | 行為說明 | `ctx == nil` 邊界行為 |
|---|---|---|
| `ambient.New[T]()` | 建立具獨立未匯出指標鍵的 `*Var[T]` | — |
| `v.Current(ctx)` | 取得目前有效值與是否存在（可區分「顯式存入零值」與「未設定／已清除」） | 回傳 `(zero, false)` |
| `v.Outer(ctx)` | 取得目前 frame 緊鄰的外層有效值 | 回傳 `(zero, false)` |
| `v.Depth(ctx)` | 取得目前有效巢狀深度（未設定或已清除時為 `0`） | 回傳 `0` |
| `v.With(ctx, val)` | Copy-on-Write 推入新不可變 frame，不修改父 `ctx` | 以 `context.Background()` 為父建立新鏈 |
| `v.Clear(ctx)` | 插入清除哨兵，隱藏外層值且不修改父 `ctx`；後續再 `With` 會從深度 `1` 重新起算 | 回傳 `context.Background()` |
| `v.Scope(ctx, val, fn)` | 以 `v.With(ctx, val)` 執行 `fn` | 同 `With`；`fn == nil` 時回傳 `nil` |
| `ambient.ScopeResult(v, ctx, val, fn)` | 泛型版本 `Scope`，直接回傳 `(R, error)` | 同 `With`；`fn == nil` 時回傳 `(zero, nil)` |

---

## 框架內建使用 `kernel/ambient` 的核心槽位

panGOlin 以下核心套件已統一採用 package-private `ambient.Var[T]`：

- `security/identity`：已驗證的 Principal 與認證方式 metadata
- `tenancy`：已核對當前資格的 Tenant 身分
- `security/scope`：`ScopeBinding` 與 `ScopeAdmitter`
- `persistence/uow`：當前 UOW 視圖（`*view`）、受控完成標記與已掛載資源槽位清單
- `persistence/database` 與 `persistence/postgres`：受控資料庫 Executor（已透過 `uow.AttachSlot` 綁定，於 `uow.Detach` 與 `RequiresNew` 自動清除隔離）
- `events/local` 與 `events/outbox`：交易綁定的本機事件佇列／派送深度與 Outbox 佇列（同上自動隔離）
- `observability`：`Telemetry`、Operation ID、Message metadata 與 SQL query 追蹤收尾函式
- `modules/organization`：私有 schema 名稱覆寫

## 下一步

- 看看 `kernel/ambient` 如何讓 **[使用交易與工作單元（UOW）](../data/unit-of-work.md)** 安全支援多執行緒平行 `RequiresNew` 與 `uow.Detach`。
- 了解 **[身分驗證](../security/authentication.md)** 與 **[資源 Scope](../security/resource-scopes.md)** 如何層層建立不可偽造的操作情境。
