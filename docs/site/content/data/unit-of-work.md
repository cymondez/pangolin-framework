---
title: 使用交易與工作單元（Unit of Work）
description: 理解 panGOlin 如何透過工作單元管理資料庫交易邊界、巢狀服務呼叫與提交結果，避免手動傳遞 *sql.Tx 帶來的維護風險。
---

> **你在架構中的位置**：`HTTP / Worker 入口 → 身分與租戶 Scope →` **`[ Application 服務：開啟 UOW 交易邊界 ]`** `→ Repository 執行 SQL → Outbox 事件 → Commit → 回傳成功`
>
> **前置建議**：先閱讀[安排你的應用程式](../concepts/architecture.md)理解分層責任；如果想先看實際跑起來的資料庫效果，可先完成 [Bookstore 教學](../tutorials/bookstore.md)。

## 為什麼需要工作單元（UOW）？

在一般的 Go 專案裡，剛開始只有一兩張表時，很多人會直接在 Service 裡寫 `tx, err := db.BeginTx(...)`，然後一路把 `*sql.Tx` 或 `pgx.Tx` 當成參數傳給每個 Repository。專案一旦變大，這種做法很快會遇到 4 個經典痛點：

1. **分層汙染**：Application 層被迫依賴具體資料庫驅動（`pgx.Tx` 或 `*sql.Tx`），單元測試很難抽換，未來切換資料庫也要大改簽名。
2. **跨服務呼叫難以共用交易**：當 `OrderService` 呼叫 `InventoryService` 時，兩邊都要開交易還是要共用同一筆交易？如果 `InventoryService` 自己 `Begin` 又 `Commit`，最後 `OrderService` 失敗時，庫存早就扣掉了，資料變成半套。
3. **容易忘記設租戶隔離或誤用 Auto-commit**：忘記開交易、誤用 Pool 直接寫入，或忘記在交易內設定 PostgreSQL RLS 變數，都可能造成資料不一致或跨租戶洩漏。
4. **業務寫入、事件發送與錯誤收尾脫鉤**：資料庫剛 Commit 就斷線、事件先發出去但 DB 回滾，或是內層錯誤被外層 `if err != nil` 吞掉後竟然還是提交成功。

panGOlin 的 **工作單元（Unit of Work，簡稱 UOW）** 用一個受控的回呼（Callback）函式 `Within` 解決這些問題：

- **Application 層只宣告「這是一筆寫入或唯讀操作」**，不碰具體連線或 `Tx` 型別。
- **交易與資源歸 Manager 獨占管理**：進入回呼前開好交易、設定好租戶 RLS，回呼順利結束後統一處理本機事件、Outbox 與 `Commit`；任何失敗（包含 panic）都由框架在有界時間內自動 `Rollback` 與釋放資源。
- **交易透過 [`kernel/ambient`](../fundamentals/ambient-scope.md) 的不可變 `context.Context` 巢狀鏈往下傳遞**：Repository 只需從傳入的 `work` context 取得當前執行器，不需要在每個方法簽名上多傳一個 `tx` 參數，同時保有跨 goroutine 的 Copy-on-Write 隔離能力。

---

## 核心心智模型：一次寫入 UOW 的完整生命週期

當你在 Application 服務呼叫 `application.CommandResult`（底層呼叫 `manager.Within`）時，框架會嚴格按照以下順序執行：

```text
呼叫端傳入已綁定 Tenant/Scope 的 ctx
  │
  ▼
[1. 准入檢查 Admission]
  ├─ 驗證 ctx 內有合法的 Tenant / Scope Binding
  ├─ 向權威來源重新確認目前資格 revision（防止已撤權者繼續寫入）
  └─ 檢查選項合法性、計算有效 Deadline
  │
  ▼
[2. 取得資源與開啟交易]
  ├─ 從連線池取得連線並 Begin 交易（唯讀且啟用 RLS 時也會開唯讀交易）
  └─ 寫入 transaction-local 的 Tenant / Owner Scope RLS 設定
  │
  ▼
[3. 執行你的業務回呼 callback(work)]
  ├─ 呼叫 Repository（透過 database.Current(work) 共用同一筆交易）
  ├─ 呼叫其他服務（若採 Required，自動加入同一根交易）
  └─ 排隊 Local Events 或寫入 Outbox（同交易保存）
  │
  ├── 回呼回傳 error / panic / 任一子範圍失敗？ ──▶ [自動 Rollback + 釋放資源] ──▶ 觸發 OnFailed / OnDisposed
  │
  ▼ 回呼回傳 nil（代表「請求完成」，尚未真正 Commit！）
[4. 完成階段 Completion]
  ├─ Flush 待寫資料 → 依 FIFO 迭代排空 Local Events
  ├─ 確認沒有未關閉的 Rows / Batch、沒有取消、沒有 rollback-only 標記
  └─ 執行唯一一次 Commit
       ├─ Commit 確認成功 ──▶ 記錄 Outcome=Committed ──▶ 釋放連線 ──▶ 觸發 OnCompleted / OnDisposed ──▶ 回傳 nil
       ├─ Commit 明確失敗 ──▶ 記錄 Outcome=RolledBack ──▶ 釋放連線 ──▶ 觸發 OnFailed / OnDisposed ──▶ 回傳 error
       └─ Commit 斷線／逾時無法確認 ──▶ 記錄 Outcome=Unknown ──▶ 釋放連線 ──▶ 回傳 UOW_COMMIT_UNKNOWN
```

> **關鍵觀念**：你的回呼函式 `return nil` 只代表「業務邏輯想提交」，**不代表資料已經落盤**。只有在 `CommandResult`／`Within` 本身回傳 `nil` 時，才代表資料庫已確認 Commit 成功。

---

## 日常怎麼寫？推薦高階 Façade 與完整範例

在撰寫日常 Application Service 時，panGOlin 提供兩組入口：

| 入口 API | 適用情境 | 為什麼推薦？ |
|---|---|---|
| `application.Command` / `CommandResult[T]` | **90% 的日常寫入 Use Case**（新增、修改、刪除） | 自動套用統一的 `ReadWrite + Transactional` 政策、補上 `use_case` Tracing Span，且 `CommandResult` 只有在 **確認 Commit 成功後** 才回傳結果 `T`（失敗時自動回傳零值，避免誤回未提交資料）。 |
| `application.Query` / `QueryResult[T]` | **90% 的日常查詢 Use Case** | 明示 `ReadOnly` 政策（配合 RLS Adapter 自動開唯讀交易）並統一接入 Tracing，直接回傳查詢結果 `T`。 |
| `application.AuthorizedCommandResult[T]` / `AuthorizedQueryResult[T]` | **需要內建授權護欄的敏感 Use Case** | 交易前先檢查入口權限，交易內再驗證 `ResourcePolicy`，直接由非 HTTP 入口呼叫也同樣受保護。 |
| `application.Runner`（`RunCommand[T]` / `RunQuery[T]`） | **單一 DI 注入點整合 UOW + 授權 + 可選冪等** | 一次注入 `Manager`、`Authorizer` 與中立 `IdempotencyExecutor`；以 `CommandSpec`／`QuerySpec` 宣告權限、UOW 選項與冪等鍵，缺少對應依賴一律 fail-closed 拒絕。 |
| `uow.UnitOfWorkManager.Within` | **進階控制情境** | 當你需要明確覆寫 `Propagation: uow.RequiresNew`、指定 `Isolation`（例如 `uow.Serializable`）或縮短單次操作 `Timeout` 時使用。 |

### 完整範例：`books/service.go`

以下是一份可直接放進你自己專案的 `books/service.go`（可與下一章[中立資料存取](database.md)的 `books/repository.go` 直接搭配編譯）。請特別留意每段程式碼的註解說明：

```go
package books

import (
	"context"
	"fmt"
	"strings"

	"github.com/cymondez/pangolin-framework/application"
	"github.com/cymondez/pangolin-framework/persistence/uow"
)

// BookStore 是 Application 層自己定義的輸出介面（Port）。
// 注意：這裡完全不出現 pgx、database/sql 或任何具體資料庫型別，
// 讓業務服務保持純淨，單元測試時也能輕鬆注入假實作（Fake/Stub）。
type BookStore interface {
	Insert(ctx context.Context, title string) error
	GetTitle(ctx context.Context, id string) (string, error)
}

// Service 代表「書籍」功能的 Application Service（負責編排 Use Case 與交易邊界）。
type Service struct {
	transactions uow.UnitOfWorkManager // 只依賴框架的 UOW 介面，不直接拿連線池
	books        BookStore             // 依賴自己定義的小介面，由外部組裝端注入實作
}

// NewService 透過建構子明確傳入所有依賴（panGOlin 不使用全域變數或隱式容器）。
func NewService(transactions uow.UnitOfWorkManager, books BookStore) *Service {
	return &Service{transactions: transactions, books: books}
}

// Create 是典型的寫入型 Use Case（Command）。
// 步驟 1：先在交易外完成純記憶體的輸入驗證，避免無效請求浪費資料庫連線。
// 步驟 2：呼叫 application.Command 開啟可寫交易，所有資料變更都在 work context 內完成。
func (s *Service) Create(ctx context.Context, title string) error {
	cleanTitle := strings.TrimSpace(title)
	if cleanTitle == "" {
		return fmt.Errorf("book title is required")
	}

	// application.Command 會以 ReadWrite + Transactional 開啟 UOW，並自動掛上 Tracing。
	// 重要：回呼內所有資料存取都必須使用參數傳入的 work，絕對不要使用外層的 ctx！
	return application.Command(ctx, s.transactions, func(work context.Context) error {
		// 這裡若後續還要寫入第二張表、發送 Outbox 事件，全部都會自動共用同一筆交易：
		// 只要其中任何一步回傳 error，框架會將整筆交易 Rollback。
		return s.books.Insert(work, cleanTitle)
	})
}

// GetTitle 是典型的查詢型 Use Case（Query）。
// 使用泛型函式 application.QueryResult[string] 直接回傳查詢結果，
// 不必在外層預先宣告 var title string 透過閉包賦值。
func (s *Service) GetTitle(ctx context.Context, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("book id is required")
	}

	// QueryResult 會以 ReadOnly 開啟 UOW：
	// - 若使用 PostgreSQL RLS Adapter，框架會自動開唯讀交易並套用租戶 RLS。
	// - 若回呼內不小心嘗試寫入資料，會直接被 UOW 以 ErrReadOnly 拒絕。
	return application.QueryResult(ctx, s.transactions, func(work context.Context) (string, error) {
		return s.books.GetTitle(work, id)
	})
}

// CreateIsolatedAudit 示範「何時該直接使用 manager.Within」：
// 當某個子操作必須「獨立於外層交易提交」（例如無論外層業務最後是否回滾，
// 都必須留下獨立的審計／紀錄），才需要透過 Within 明示 Propagation: uow.RequiresNew。
func (s *Service) CreateIsolatedAudit(ctx context.Context, title string) error {
	return s.transactions.Within(ctx, uow.Options{
		Mode:        uow.ReadWrite,
		Propagation: uow.RequiresNew, // 開立獨立根 UOW、新連線與交易並立刻獨立提交（不修改父視圖）
	}, func(work context.Context) error {
		return s.books.Insert(work, "[audit] "+strings.TrimSpace(title))
	})
}
```

### 逐段導讀：為什麼要這樣寫？

1. **為什麼 `BookStore` 介面宣告在 `books` 套件裡，而不直接用框架的通用 CRUD？**
   panGOlin 提倡依業務需求定義精準的小介面（Port），例如 `Insert`、`GetTitle`。這讓 Application 層與 PostgreSQL／MySQL／SQLite 完全解耦；組裝端怎麼實作這個介面，請見下一章[中立資料存取與資料庫選擇](database.md)。
2. **為什麼輸入驗證放在 `application.Command` 之前？**
   開啟 UOW 會向資料庫連線池借用連線並開啟交易（以及執行權威資格檢查）。把不需碰 DB 的格式驗證放在外面，能縮短持鎖與連線占用時間，避免無效請求拖垮吞吐量。
3. **為什麼回呼裡必須傳 `work`，不能傳外層的 `ctx`？**
   外層的 `ctx` 只有身分與租戶 Scope，**還沒有掛上 UOW 交易視圖**。Repository 呼叫 `database.Current(work)` 時是從 `work` 取出受控的交易執行器；如果你手誤傳成外層 `ctx`，Repository 會因為找不到有效 UOW 而直接報錯拒絕（不會偷偷退回連線池 Auto-commit）。
4. **為什麼查詢也一定要包 `QueryResult`？直接拿 Pool `SELECT` 不行嗎？**
   不行。在多租戶與 RLS 架構下，連「讀取」都必須在交易中設定 transaction-local 的租戶／Owner 變數，才能確保資料庫層強制攔截跨租戶讀取。因此 panGOlin 規定：沒有有效 UOW Scope 時，業務 Repository 連讀取都會拒絕。
5. **這段程式要怎麼跟真實資料庫接起來？**
   組裝端會建立 `database.Resource` 與 `uow.NewManager`，傳給 `NewService`；呼叫 `Create`／`GetTitle` 前，由 HTTP 或 Worker 入口先建立[驗證過的租戶 context](../security/multi-tenancy.md)。你可以對照套件內的 `examples/bookstore/application/books.go`，它正是使用 `application.CommandResult` 將「寫入書籍」與「寫入 Outbox 事件」包在同一筆交易裡。

---

## 服務呼叫服務：理解交易傳播（`Required` vs `RequiresNew`）

真實專案中，Service A 常會呼叫 Service B，而兩邊各自都包了 UOW。這時你需要決定它們的交易關係：

### 1. `uow.Required`（預設）：共生死，共用同一筆根交易

```text
Service A: Command (ReadWrite) ─────────[ 根交易 #1 開始 ]─────────┐
   │                                                               │
   ├─ 寫入訂單 (SQL 1)                                             │
   ├─ 呼叫 Service B: Command (Required) ──▶ 加入根交易 #1          │
   │     ├─ 扣減庫存 (SQL 2)                                       │
   │     └─ 若這裡回傳 error ──▶ 根交易 #1 標記 rollback-only！      │
   │        （即使 Service A 吞掉 error，最後依然會整筆 Rollback）   │
   └─ Service A 回呼結束 ──────────────────────────────────▶ 統一 Commit / Rollback
```

- **子範圍結束時不會提交**：只有最外層的根範圍結束，才會執行唯一的 Commit。
- **Rollback-only 不可逆**：只要內層失敗（或選項衝突），根交易就會被標記為 rollback-only。**就算外層刻意忽略或捕捉內層錯誤，外層仍不可能把這筆交易提交成功**——這徹底杜絕了「子服務寫壞一半、父服務還硬把它提交」的資料損毀問題。
- **唯讀子視圖權限縮限**：可寫根交易內呼叫 `Query`（唯讀 Required）是允許的，但該唯讀子視圖內**禁止寫入**，且子回呼結束後，子 `work` context 立即失效（父 `work` 仍可繼續使用）。
- **不可升級為可寫**：外層是 `ReadOnly` 時，內層如果要求 `ReadWrite` + `Required`，會因為選項衝突直接失敗（`ErrOptionsConflict`）。

### 2. `uow.RequiresNew`：開立獨立交易與連線（支援平行多執行緒）

```text
Service A: Command (ReadWrite) ──[ 根交易 #1 (連線 #1) ]───────────────────────▶ 若父失敗：Rollback #1
                                          │
                                          ├── goroutine 1: RequiresNew ──[ 獨立交易 #2 (連線 #2) ]──▶ 獨立 Commit #2
                                          └── goroutine 2: RequiresNew ──[ 獨立交易 #3 (連線 #3) ]──▶ 獨立 Commit #3
```

- 適合「**即使主流程最後失敗，這筆紀錄仍要保留**」的場景（例如獨立安全事件、扣點失敗紀錄、外部回呼收件紀錄），以及「**在一個已驗證的父操作內，安全地以多條 goroutine 平行執行獨立資料庫工作**」。
- **不可變 Ambient UOW 模型**：`RequiresNew` 透過 [`kernel/ambient`](../fundamentals/ambient-scope.md) 的 Copy-on-Write 鏈建立全新的獨立根 UOW 與獨立連線，**不會回寫或暫停父節點狀態**。因此多個 goroutine 可以對同一個有效的父 `work` context **同時併發呼叫 `Within(..., RequiresNew)`**，共用同一個無狀態 Repository 實例、各自持有獨立交易與連線，互不鎖死、互不汙染。
- **三個務必牢記的限制**：
  1. **它不是 Savepoint**：子交易已經 Commit 的資料，不會因為父交易稍後回滾而撤銷；同時子交易也**看不到**父交易尚未 Commit 的資料。
  2. **需要額外連線且小心鎖衝突**：每個同時進行的 `RequiresNew` 都會向連線池借用一條獨立連線。請確保連線池容量足以負荷併發度；若子交易試圖更新父交易已經鎖住的同一筆資料列（或 SQLite 單一 writer 寫鎖），會依 `Timeout` 失敗，不會退回共用父交易。
  3. **不可藉此偷渡權限或延長時限**：`RequiresNew` 會沿用父操作的 Tenant、Principal、Scope Binding 與剩餘 Deadline，深度受 `MaxRequiresNewDepth` 限制；且若父 UOW 已進入完成階段或已關閉，將 fail-closed 拒絕（`ErrBusy`／`ErrClosed`）。若在父 UOW 內啟動平行 `RequiresNew` goroutine，務必以 `sync.WaitGroup` 在父回呼返回前等待它們完成。

### 3. `uow.Detach(ctx)`：脫離目前 UOW，啟動獨立頂層操作

如果你需要在平行 goroutine 中啟動一筆**完全獨立的頂層 UOW**（不受父 UOW 生命週期與 `MaxRequiresNewDepth` 限制，並且希望在進入前重新執行 `scope.Admit` 核對當前最新權威 revision），請呼叫 `uow.Detach(ctx)`：

```go
// Detach 會透過 ambient.Var.Clear 清除目前 UOW 視圖、完成標記與所有已掛載的資源槽位
// （包含 database／postgres Executor 與 Local／Outbox 佇列），絕不漏用父交易連線；
// 同時完整保留已驗證的 Identity、Tenant、ScopeBinding、Telemetry 與 Deadline。
detached := uow.Detach(work)

// 可將 detached 安全交給平行 goroutine 各自啟動獨立頂層 UOW（各自重新執行 scope.Admit）。
err := application.Query(detached, s.transactions, func(topWork context.Context) error {
	return s.books.CheckSomething(topWork)
})
```

> **動手驗證**：想親眼看到「父回滾、子提交、舊 Scope 失效」在真實 PostgreSQL 上的行為，請執行[交易傳播教學](../tutorials/transactions.mdx)。

---

## 常見踩坑與反例對照（新人必看）

以下是從傳統 Go 專案轉入 panGOlin 時最常踩的 5 個地雷：

### 地雷 1：在「同一筆 UOW」直接平行下 SQL，或讓背景 goroutine 繼續使用已結束的 `work`

```go title="反例：切勿不開新 UOW 就共用同一條連線平行查 DB ❌"
application.Command(ctx, s.transactions, func(work context.Context) error {
	// ❌ 錯誤 1：同一筆 UOW 綁定單一資料庫連線（底層單一連線同一時間只能執行一條 SQL），
	//    在同一個 work 上直接開 goroutine 平行呼叫 Repository 會觸發 ErrBusy 並使交易回滾！
	// ❌ 錯誤 2：沒有等 goroutine 結束就讓回呼返回，連線關閉後背景 goroutine 會拿到 ErrClosed！
	go func() {
		_ = s.books.Insert(work, "async-book")
	}()
	return s.books.Insert(work, "sync-book")
})
```

**✅ 正確做法（依需求二選一）**：
1. **需要平行資料庫操作（同一請求內併發查詢／獨立寫入）**：共用同一個無狀態 Repository，在每個 goroutine 內透過 **`Propagation: uow.RequiresNew`**（需等待 `wg.Wait()` 再結束父回呼）或 **`uow.Detach(work)` + 新頂層 `Within`** 取得各自獨立的 UOW 與資料庫連線！
2. **需要跨越請求生命週期的可靠背景工作**：在同交易內寫入 [Outbox](../events/reliable-delivery.md) 或[持久化背景工作](../platform/jobs.md)，由獨立 Worker 另開新 UOW 執行。

### 地雷 2：內層失敗後吞掉錯誤，以為外層還能成功提交

```go title="反例：吞掉 Required 子交易的錯誤 ❌"
application.Command(ctx, s.transactions, func(work context.Context) error {
	if err := s.inventory.Reserve(work, itemID); err != nil {
		// ❌ 即使這裡不 return err，Reserve 裡面的 Required 子交易失敗已把根標成 rollback-only！
		// 這個回呼最後 return nil 時，Commit 仍會失敗並回傳 ErrRollbackOnly。
		log.Printf("reserve failed, ignore: %v", err)
	}
	return s.orders.Save(work, order)
})
```

**✅ 正確做法**：如果子步驟「失敗可容許且不該影響主交易」，就不要讓它跑在同一個失敗會污染交易的 `Required` 內，應先檢查條件或改用 `RequiresNew` 隔離；若屬於同一個原子業務，則必須誠實 `return err`。

### 地雷 3：在 `OnCompleted` 回呼裡操作資料庫或做不可丟失的副作用

```go title="反例：在完成回呼裡繼續寫 DB 或寄關鍵通知 ❌"
scope, _ := s.transactions.Current(work)
_ = scope.OnCompleted(func(hookCtx context.Context, c uow.Completion) error {
	// ❌ OnCompleted 執行時原交易與連線已釋放，hookCtx 拿不到舊的 UOW Executor！
	// ❌ 而且若這裡寄信失敗或程式剛好崩潰，交易早已 Commit，這封信就永遠消失了。
	return s.mailer.SendReceipt(hookCtx, orderID)
})
```

**✅ 正確做法**：`OnCompleted`／`OnFailed`／`OnDisposed` 只適合做「可容許遺失的本機觀測」（例如遞增 Metrics、寫本機快取失效提示）。凡是**必須可靠執行**的後續工作（寄信、同步其他模組、呼叫外部金流），務必在交易內寫入 [Outbox](../events/reliable-delivery.md)。

### 地雷 4：忽略 `UOW_COMMIT_UNKNOWN`，遇到錯誤就無條件重試寫入

網路抖動或資料庫在 Commit 回應瞬間重啟時，交易**可能已經成功寫入，也可能沒寫入**。這時 panGOlin 不會假裝它沒發生，而是明確回傳 `uow.ErrCommitUnknown`（`Outcome() == uow.Unknown`）。

```go title="正例：辨識 UOW_COMMIT_UNKNOWN 並走對帳流程 ✅"
err := s.books.Create(ctx, title)
if err != nil {
	if errors.Is(err, uow.ErrCommitUnknown) {
		// ✅ 切勿自動重跑 Create！
		// 透過 uow.Error 取得 OperationID，搭配冪等鍵查詢或讓用戶端以原 Idempotency Key 對帳。
		var uowErr uow.Error
		if errors.As(err, &uowErr) {
			log.Printf("commit outcome unknown: op=%s scope=%s", uowErr.OperationID(), uowErr.ScopeID())
		}
		return err
	}
	return err
}
```

> 完整的重複提交防護與對帳做法，請見[Command 冪等與結果對帳](idempotency.md)。

### 地雷 5：誤把 `SaveChanges` 當成 `Commit`

`scope.SaveChanges(work)` 只負責把緩衝中的變更 flush 到目前交易（pgx 的 SQL 通常已即時送進交易），**它不會提交交易、不會派送 Local Events、也不會觸發完成回呼**；之後若回呼失敗，先前 flush 的資料仍會整筆回滾。

---

## 進階選項、期限與支援邊界

### 1. `uow.Options` 常用選項速查

| 選項 | 可選值與預設 | 實務說明與限制 |
|---|---|---|
| `Mode` | 預設 `AccessDefault`（等效 `ReadOnly`）、`ReadOnly`、`ReadWrite` | 未指定時預設唯讀，防止忘記宣告卻意外寫入。 |
| `Transaction` | 預設 `TransactionAuto`、`Transactional`、`NonTransactional` | `ReadWrite` 強制開交易；啟用 transaction-local RLS 的 Adapter 連唯讀也自動開交易。`NonTransactional` 僅適用於不依賴 RLS 的唯讀資源，**不支援非交易寫入**。 |
| `Propagation` | 預設 `Required`、`RequiresNew` | 同一個操作中，兩者皆不可切換 Tenant、Principal 或 ScopeRef。 |
| `Isolation` | 預設 `IsolationDefault`、`ReadCommitted`、`RepeatableRead`、`Serializable` | `Required` 子範圍若明示級別，必須與父交易一致；使用 SQLite 時須於 Manager 明示 `DefaultIsolation: uow.Serializable`。 |
| `Timeout` | 零值（繼承預設或父剩餘期限）或正值 | 子範圍設定正值**只能縮短**期限，不能延長父操作期限。連線取得、Begin、SQL、完成階段與 Commit 全程受有效 Deadline 控管。 |

### 2. Manager 啟動設定（`uow.DefaultOptions`）

組裝端呼叫 `uow.NewManager(resource, opts)` 時，以下四項皆必須為正值（否則啟動直接拒絕）：

- `DefaultTimeout`：頂層操作的預設總期限。
- `CleanupTimeout`：交易失敗或逾時後，執行 Rollback 與資源清理的獨立有界期限（避免主 context 已取消導致 Rollback 無法執行）。
- `HookTimeout`：`OnCompleted`／`OnFailed`／`OnDisposed` 每個觀察器的獨立逾時上限；觀察器錯誤或 panic 只會記錄，絕不會把已確認 Commit 的交易改寫成失敗。
- `MaxRequiresNewDepth`：`RequiresNew` 允許的最大巢狀深度上限，防止無窮遞迴耗盡連線池。

### 3. 重試原則與串流邊界

- **Manager 絕不自動重跑回呼**：只有上層明確判定「已確認回滾（非 `Unknown`）」、「Use Case 可重試」且具備有界退避時，才由外層開啟新的根 UOW 重試整個 Use Case。
- **HTTP 與串流**：HTTP 寫入端點可搭配共用 `transport/http.Command` 確保「Commit 成功後才回傳 2xx」（它會拒絕加入外層既有 UOW）；SSE／WebSocket 等長連線應針對每次互動呼叫 `StreamOperation` 開啟短 UOW，**切勿用一筆長交易包住整條串流連線**。
- **0.1.0 明確不提供的功能**：非交易業務寫入、Savepoint、Suppress（暫停交易改用無交易）、跨資料庫分散式原子提交、隱式自動重試。完整邊界見[功能相容性](../reference/compatibility.md)。

---

## 導入檢查清單與下一步

在把 UOW 導入你的第一個業務模組前，請快速核對：

- [ ] Application Service 只依賴 `uow.UnitOfWorkManager` 與自己的 Repository 介面，沒有 import `pgx` 或 `database/sql`。
- [ ] 日常寫入統一用 `application.Command`／`CommandResult`，查詢統一用 `application.Query`／`QueryResult`。
- [ ] 回呼內全部使用傳入的 `work` context，沒有混用外層 `ctx`；若需多執行緒平行操作 DB，已為每個 goroutine 開立獨立的 `RequiresNew` 或 `uow.Detach` 頂層 UOW（沒有在同一筆 UOW 內共用單一連線平行下 SQL）。
- [ ] 沒有在 `OnCompleted` 執行需要可靠保證的寫入或外部通知。
- [ ] 針對付款、建單等關鍵寫入，規劃好 [Command 冪等](idempotency.md) 與 `UOW_COMMIT_UNKNOWN` 的對帳處理。

**推薦閱讀順序**：
1. [中立資料存取與資料庫選擇](database.md)：接著實作上面 `BookStore` 對應的 `books/repository.go`，學習如何在 PostgreSQL／MySQL／SQLite 間保持程式碼中立。
2. [交易傳播教學](../tutorials/transactions.mdx)：用真實 PostgreSQL 親手驗證 `Required` 與 `RequiresNew`。
3. [組裝可靠事件配送](../events/reliable-delivery.md)：學習如何把 Local Event 與 Outbox 掛進 UOW 的完成階段。
