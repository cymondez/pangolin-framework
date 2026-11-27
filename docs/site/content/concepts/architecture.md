---
title: 安排你的應用程式（架構總覽）
description: 建立 panGOlin 的核心心智模型：理解四層架構責任、模組啟動生命週期、單一寫入請求管線，以及如何從零新增自己的業務模組。
---

剛接觸 panGOlin 時，最容易迷失的不是某個 API 的參數，而是「**這些模組、DI、Scope、UOW、Outbox 之間到底是什麼關係？**」。

這一頁提供全站共用的**架構心智地圖**。後續每一篇功能指南都會標示它位於這張地圖的哪一環，方便你隨時對照全局。

---

## 為什麼 panGOlin 強調「明確邊界」？

在小型 Go 程式中，所有程式碼寫在 `main.go` 裡毫無問題。但當一個系統同時有會員、商品、訂單、計費與多租戶需求時，如果缺乏邊界規則，通常會演變成三種失控：

1. **隱式相依與循環引用**：A 模組直接查詢 B 模組的資料表，導致任何欄位重構都牽一髮動全身。
2. **橫切關注點散落業務碼**：每個 Handler 自己解析 JWT、自己檢驗租戶、自己 `db.Begin()`、自己記稽核日誌，只要漏掉一個端點就變成安全漏洞。
3. **全域狀態難以測試與拆服**：把 `*pgxpool.Pool` 或「當前使用者」塞進全域變數，導致測試互相汙染，未來也拆不開服務。

panGOlin 的核心解法可以濃縮成三句話：
- **先按「功能模組（擁有者）」切塊，再在模組內按「分層」切責任**。
- **所有依賴由建構子明確傳入，所有請求狀態由 `context.Context` 受控傳遞**（沒有全域 Current、沒有魔法反射攔截器）。
- **跨模組只能走公開介面契約（Contracts）或版本化事件**，禁止直接讀寫別人的私有表或內部套件。

---

## 心智地圖 1：專案目錄與五個職責層

當你的應用從單檔成長為多模組專案時，建議採用以下結構（panGOlin 套件直接以 Go `import` 引用，不須手寫 `src/`）：

```text
my-app/
  cmd/
    api/main.go             ← [入口與組裝層] 讀取環境、建立 DB/Broker、將根模組交給 app.Run
    worker/main.go          ← [背景入口] 獨立啟動 Outbox/Inbox/Jobs Worker
  internal/
    books/                  ← [功能模組 A：書籍]（先按擁有者切開）
      contracts/            ← 公開給其他模組呼叫的穩定介面與 DTO（僅跨模組需要時建立）
      domain/               ← 業務實體、值物件與不變條件（不依賴 DB/HTTP/框架 Runtime）
      application/          ← Use Case 編排、權限檢查、開啟 UOW 交易邊界
      infrastructure/       ← 實作 Repository Port、參數化 SQL、事件 Adapter
      transport/http/       ← HTTP 路由、解碼請求、呼叫 Application、映射狀態碼
      module.go             ← 宣告 ModuleMetadata、DependsOn、Configure（DI 註冊）與 Start/Stop
    orders/                 ← [功能模組 B：訂單]（只能透過 books/contracts 或事件與 books 互動）
  go.mod
```

### 每一層「應該做什麼」與「絕對不做什麼」

以「建立一本書」為例，各層分工與禁止事項如下：

| 層級 | 負責的工作（應該做 ✅） | 嚴格邊界（不要做 ❌） |
|---|---|---|
| **Transport（HTTP / Worker）** | 解碼 JSON、驗證請求格式、透過管線驗證身分與 Tenant／Scope、呼叫 Application、把結果或錯誤轉成 HTTP 狀態碼 | 不要在 Handler 裡寫 SQL、不要直接操作業務資料表、不要在尚未確認 Commit 前回傳 2xx |
| **Application（Use Case）** | 協調業務流程、驗證業務操作權限（`policy.Require`）、以 `application.CommandResult`／`QueryResult` 開啟 UOW、呼叫 Domain 與 Repository Port、發送事件 | 不要 `import` 具體 DB 驅動（`pgx`/`database/sql`）、不要 `import` HTTP 套件（`net/http`） |
| **Domain（領域模型）** | 維護 Entity、Value Object 與業務不變條件（例如書名不可空白、狀態轉換規則） | 只依賴 Go 標準函式庫與極薄共用基元；不要碰 DB、網路、設定檔或框架 Runtime |
| **Infrastructure（基礎設施）** | 實作 Domain／Application 定義的 Repository Port（透過 `database.Current(ctx)` 執行參數化 SQL）、實作外部服務 Client | 不要自己呼叫 `pool.Begin()` 繞過 UOW、不要把 `work` context 存起來跨請求使用 |
| **Composition Root（`cmd/` + `module.go`）** | 挑選具體資料庫／認證實作、宣告模組相依圖（`DependsOn`）、註冊 DI 建構子、設定逾時與啟動檢查 | 不要把業務判斷規則塞進啟動組裝程式 |

> **小專案怎麼起步？** 剛建立第一個程式時不需要馬上開五個資料夾。可以像[問候服務教學](../tutorials/greeting-application.mdx)一樣先放在單一 `main.go`，等業務規則變多、需要資料庫或第二個模組時再自然拆分。

---

## 心智地圖 2：應用啟動與優雅停機生命週期

當你在 `main.go` 呼叫 `app.Run(ctx, rootModule)` 時，Runtime **不是**「一個模組一次跑完所有步驟」，而是**全部模組同步推進每個階段**：

```text
app.Run(ctx, rootModule, opts...)
  │
  ├─ [1. 規劃相依圖]  沿著 DependsOn() 收集所有模組，拒絕重名、循環、缺依賴與版本不符
  │                   依拓撲排序排定啟動層級（同層依名稱穩定排序）
  │
  ├─ [2. 組態三階段]  全部模組依序完成 PreConfigure ──▶ 全部 Configure ──▶ 全部 PostConfigure
  │                   （此階段只宣告預設設定與註冊 DI 建構子，尚未解析服務）
  │
  ├─ [3. 封存與驗證]  合併設定（預設 < 檔案 < 環境 < CLI）並驗證必要欄位
  │                   Seal DI 容器（先靜態檢查缺依賴、循環、Singleton 誤捕獲 Operation）
  │                   執行 StartupChecks（例如檢查 DB Schema 相容性與 Scope Provider）
  │
  ├─ [4. 依序啟動]    依相依順序（被依賴者先）逐模組呼叫 Start(ctx, rt)
  │                   全部成功後應用進入 Ready 狀態，開始對外服務
  │
  ▼  收到 Ctrl+C / SIGTERM（ctx 取消）
  │
  ├─ [5. 優雅停機]    Quiesce（停止接收新請求／新工作認領）
  │                   ──▶ Drain（在 DrainTimeout 內等待進行中請求完成）
  │                   ──▶ 反序呼叫各模組 Stop（先停業務模組，最後停 DB／Broker）
  │                   ──▶ 依反序執行 DI Cleanup 釋放資源
```

**為什麼要分成「全部 Configure 完才一起 Seal、再依序 Start」？**
因為這樣在啟動任何連線或監聽任何 Port **之前**，框架就能一次抓出所有設定缺漏、DI 循環相依與跨模組可見性違規——**要失敗就在第 0 秒乾淨失敗，絕不啟動出一個跑一半的殘缺服務**。

---

## 心智地圖 3：一筆寫入請求的完整執行管線

了解模組如何啟動後，接下來看一個真實的 HTTP 寫入請求（例如 `POST /books`）是如何流過 panGOlin 各個防線的：

```text
外部 HTTP 寫入請求（例：POST /books）
  │
  ▼
[1. Trace / Timeout]        建立 OpenTelemetry Span 與整體請求逾時
  ▼
[2. Authenticate]           由 CredentialExtractor + identity.Pipeline 驗證 JWT／Session
                            （失敗回 401；此處只回答「你是誰」）
  ▼
[3. Resolve Tenant]         比對 Token 與候選 Header，查詢當前資格 Authority 確認未撤權
                            （跨租戶或已停權回 403；Header 永遠只是候選值！）
  ▼
[4. Resolve / Bind Scope]   綁定不可變的 ScopeRef (TenantID, Kind, ID) 至 context
  ▼
[5. Entry Policy]           檢查入口層 Permission（預設拒絕，User 明確 Deny 優先）
  ▼
[6. Decode / Validate]      解析請求 DTO 與格式驗證（格式錯直接回 400，不浪費 DB 連線）
  ▼
[7. 開啟 UOW (Command)]     向權威重驗 revision → 開啟可寫交易 → 寫入 transaction-local RLS
  │   ├─ [7a. Resource Policy] 若需依資料內容授權（如「僅作者可改」），載入後檢查
  │   ├─ [7b. Domain & Repo]   執行領域邏輯，透過 database.Current(work) 寫入資料
  │   └─ [7c. Local / Outbox]  排隊本機事件；可靠跨程序事件同交易寫入 Outbox 表
  ▼
[8. Completion & Commit]    Flush → 排空 Local Events → 唯一一次 Commit
                            （只有確認 Commit 成功才繼續！若未知回 UOW_COMMIT_UNKNOWN）
  ▼
[9. 序列化與回傳 2xx]        將結果寫回 HTTP Response（背景 Worker 另行非同步派送 Outbox）
```

> **架構鐵律**：這條管線的任何一個環節都不可跳過或顛倒。例如不能拿請求 body 自報的 `tenant_id` 寫進資料庫、不能在交易 Commit 之前回傳 `201 Created`、也不能在交易還沒成功前直接發送 NATS 訊息。

---

## 如何在自己專案新增一個業務模組？（6 步實戰清單）

當你準備用 panGOlin 開發專案裡的第一個真實業務功能（例如「訂單 `orders`」）時，照著這個順序做最不容易走彎路：

1. **先寫 Domain 與不變條件**（`internal/orders/domain/`）：定義資料結構、建構函式驗證規則與自己的 `Repository` 介面（Port），不引入任何框架 DB 套件。
2. **撰寫 Application Use Case**（`internal/orders/application/`）：透過建構子接收 `uow.UnitOfWorkManager` 與 `domain.Repository`，用 `application.CommandResult`（寫入）與 `application.QueryResult`（查詢）包住交易邊界，敏感操作加上 `policy.Require`。
3. **實作 Infrastructure Repository**（`internal/orders/infrastructure/`）：實作 `domain.Repository`，統一從回呼傳入的 `work` context 呼叫 `database.Current(work)` 與 `database.CurrentOwner(work)` 執行參數化 SQL，並撰寫對應的 SQL Migration（含租戶／Owner 條件與唯一鍵）。
4. **掛接 Transport 入口**（`internal/orders/transport/http/`）：使用 `transport/http` 管線完成驗證、租戶解析與解碼，再呼叫 Application Service。
5. **封裝成 panGOlin Module**（`internal/orders/module.go`）：宣告 `ModuleMetadata()`、`DependsOn()`，在 `Configure` 註冊建構子；若其他模組需要呼叫訂單服務，才把介面放進 `internal/orders/contracts/` 並由 `orders` 模組呼叫 `b.Services().Export(...)`。
6. **掛回根模組並驗證**：在應用的根模組 `DependsOn()` 加入 `module.Of[orders.Module]()`，執行 `go test ./...` 與啟動檢查。

---

## 參照可執行的真實範例

如果你想立刻看到上述五層如何落實成真實 Go 程式碼，請直接打開套件內建的 `examples/bookstore`，建議**依這個順序讀碼**：

1. `examples/bookstore/domain/book.go` — 看純粹的領域規則怎麼寫。
2. `examples/bookstore/application/books.go` — 看 `CommandResult`／`QueryResult` 如何串起 Repository 與 Outbox 事件。
3. `examples/bookstore/infrastructure/` — 看 Repository 如何從 UOW 取得受控執行器。
4. `examples/bookstore/transport/http/books.go` — 看 HTTP 請求如何通過驗證與租戶隔離後才呼叫 Application。
5. `examples/bookstore/bootstrap/` — 看最後如何把全部元件組裝成模組與 Host。

## 下一步

- 剛跑完第一個程式？接著實作 **[建立可設定的問候服務](../tutorials/greeting-application.mdx)**，親手體驗模組、DI 與組態。
- 想直接深入核心機制？依序閱讀 **[功能模組](../fundamentals/modules.md)** → **[依賴注入](../fundamentals/dependency-injection.mdx)** → **[使用交易與工作單元](../data/unit-of-work.md)**。
