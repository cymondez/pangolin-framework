---
title: Bookstore：建立有登入與資料庫的 API
description: 啟動完整的 Bookstore API 與獨立事件 Worker，親手驗證 JWT 登入、PostgreSQL 交易提交、跨租戶 403 攔截與非同步事件投影，並學會如何閱讀範例原始碼。
---

> **你在架構中的位置**：**`[ 完整端到端系統演練：HTTP API + JWT IdP + PostgreSQL (UOW/RLS/Outbox) + NATS JetStream + 獨立 Worker ]`**
>
> **兩種 Bookstore 的區別**：本頁操作的是**框架原始碼內建的完整雙程序範例（`examples/bookstore`）**，建立書籍回傳 **HTTP 201** 且回應含 `version` 欄位；如果想直接用 CLI 生成你自己的獨立專案骨架（建立／讀取皆回 HTTP 200、欄位精簡為 `id`/`title`），請前往 **[用 CLI 建立自己的 Bookstore](generated-bookstore.md)**。

完成這個教學後，你會親眼看到一個真實的多租戶 panGOlin 系統如何運作：

```text
[終端 3: 你的 PowerShell]
   ├─ 1. 向本機示範 IdP (18180) 登入取得 alice@tenant-a 的 RS256 JWT
   ├─ 2. 帶 Token 呼叫 Bookstore API (18181) 建立書籍
   │       └─▶ [終端 1: API (帳號 pangolin_app，受 RLS 保護)]
   │             同交易寫入 books 表 + Outbox 事件 ──Commit─▶ 回傳 HTTP 201
   ├─ 3. 嘗試帶 X-Tenant-ID: tenant-b 跨租戶偷讀 ──▶ 直接被攔截回傳 HTTP 403！
   └─ 4. [終端 2: 背景 Worker (帳號 pangolin_worker)]
           自動認領 Outbox ──▶ 送往 NATS (14322) ──▶ 更新背景查詢投影！
```

---

## 需要準備什麼？

- 已取得 `pangolin-framework` 原始碼，並完成[第一個應用](../getting-started/quickstart.mdx)。
- **Docker Desktop**（或可用的 Docker Linux Engine）與 Docker Compose。
- **PowerShell 7（`pwsh`）**：本頁命令跨 Windows／macOS／Linux 皆可用 `pwsh` 執行。
- 確認本機以下 4 個 loopback port 沒有被其他服務占用：`15542`（PostgreSQL）、`14322`（NATS）、`18180`（示範 IdP）、`18181`（Bookstore API）。

> **環境變數作用域提醒**：以下 `$env:` 設定只對**當前終端機視窗**有效，三個終端請各自設定該段落列出的變數。

---

## 1. 準備示範服務

在框架原始碼的 **`pangolin-framework` 根目錄**執行：

```powershell
pwsh ./examples/bookstore/scripts/demo.ps1 up
```

這個腳本會先確認 Docker 環境，接著建立專屬於本教學的隔離 PostgreSQL、NATS JetStream 與本機示範 IdP，並完成書籍資料表初始化。看到 **`Bookstore services are ready`** 即可繼續。

> **僅供本機教學**：示範 IdP 允許你指定任意 `subject`/`tenant_id` 簽發測試 Token，示範密碼也僅限本機隔離環境使用，切勿用於正式環境。

---

## 2. 第一個終端：啟動 Bookstore API

保持在 `pangolin-framework` 根目錄執行（注意 API 使用的是受 RLS 限制的最小權限帳號 **`pangolin_app`**）：

```powershell
$env:PANGOLIN_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_ISSUER = 'http://127.0.0.1:18180'
go run ./examples/bookstore/cmd/api -listen 127.0.0.1:18181
```

API 會啟動在 `http://127.0.0.1:18181` 並持續運行。**保留這個終端不要關**，另開第二個終端。

---

## 3. 第二個終端：啟動獨立背景 Worker

切到 `pangolin-framework` 根目錄，以獨立的 **`pangolin_worker`** 角色啟動背景 Worker：

```powershell
$env:PANGOLIN_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_NATS_URL = 'nats://127.0.0.1:14322'
$env:PANGOLIN_NATS_STREAM = 'BOOKSTORE_DEMO'
$env:PANGOLIN_NATS_SUBJECT = 'bookstore.demo.book_created'
$env:PANGOLIN_WORKER_TENANTS = 'tenant-a,tenant-b'
go run ./examples/bookstore/cmd/worker
```

**為什麼 API 與 Worker 要分兩個程序、用兩個不同 DB 帳號？**
這展示了 panGOlin 的最小權限分離原則：API 帳號專注於受 RLS 限制的線上業務讀寫；Worker 帳號負責在背景配送已提交的 Outbox 事件並更新 Read Model，兩者互不搶資源且可各自水平擴展。同樣保留此終端，再開第三個終端。

---

## 4. 第三個終端：登入取得 RS256 Token

在第三個終端向本機示範 IdP 取得 `alice`（隸屬 `tenant-a`）的 JWT：

```powershell
$identity = @{ subject = 'alice'; tenant_id = 'tenant-a' } | ConvertTo-Json
$login = Invoke-RestMethod -Method Post -Uri http://127.0.0.1:18180/token `
  -ContentType application/json -Body $identity
$headers = @{ Authorization = "Bearer $($login.access_token)" }
```

> 本機教學 Token 有效期為 **5 分鐘**；若稍後遇到 `401`，重新執行這段即可換發新 Token。

---

## 5. 建立並讀回一本書

**在同一個第三終端**（保留 `$headers`）建立一本新書並立刻查詢：

```powershell
$bookId = [guid]::NewGuid().ToString()
$body = @{ id = $bookId; title = 'My first panGOlin book' } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:18181/books `
  -Headers $headers -ContentType application/json -Body $body
Invoke-RestMethod -Uri "http://127.0.0.1:18181/books/$bookId" -Headers $headers
```

你會看到回傳相同的 `id`、`title` 與 `version: 1`（建立回 HTTP **201**、讀取回 **200**）。背後發生的關鍵保證是：**API 必須等 PostgreSQL 確認書籍與 Outbox 事件同交易 Commit 成功後，才會送出 201 回應**；同時第二終端的 Worker 會自動把事件送往 NATS 並投影。

---

## 6. 試試跨租戶存取（驗證安全防線）

繼續拿 `alice`（`tenant-a`）的合法 Token，刻意在 Header 偽造 `X-Tenant-ID: tenant-b` 嘗試跨租戶讀取：

```powershell
$wrongTenant = @{
  Authorization = "Bearer $($login.access_token)"
  'X-Tenant-ID' = 'tenant-b'
}
try {
  Invoke-RestMethod -Uri "http://127.0.0.1:18181/books/$bookId" -Headers $wrongTenant
} catch {
  [int]$_.Exception.Response.StatusCode
}
```

預期輸出 **`403`**。這證明了 panGOlin 的核心原則：**HTTP Header 永遠只是候選提示（Hint），絕不能用來取得另一個租戶的權限**。

---

## 7. 深入原始碼：資深工程師建議的讀碼路線

跑完之後，最有效的學習方式是沿著請求路徑打開 `examples/bookstore` 裡這 5 支檔案（建議照順序讀）：

1. **`examples/bookstore/domain/book.go`**：看純 Go 領域模型如何把書名驗證等不變條件鎖在 Domain 層，零外部依賴。
2. **`examples/bookstore/application/books.go`**：看 `Books.Create` 與 `Books.Get` 如何使用 **`application.CommandResult`／`QueryResult`**，把「儲存書籍」與「寫入 Outbox 事件」包在同一個原子交易中。
3. **`examples/bookstore/infrastructure/`**：看 Repository 如何從 UOW 取得受控執行器，不自行 `Begin`。
4. **`examples/bookstore/transport/http/books.go`**：看 HTTP Handler 如何把 JWT 驗證、Tenant 解析與 DTO 解碼串成安全管線。
5. **`examples/bookstore/bootstrap/`**：看最後如何在啟動組裝層把所有元件接成 panGOlin Module。

你可以試著改一下 `domain/book.go` 的書名長度驗證，重啟 API 測試看看 `422 VALIDATION_FAILED` 錯誤回應！

---

## 8. 結束教學與停止服務

對 API 與 Worker 的終端各按 `Ctrl+C`（觀察優雅停機日誌），接著回到 `pangolin-framework` 根目錄關閉示範容器：

```powershell
pwsh ./examples/bookstore/scripts/demo.ps1 down
```

此命令只會停止本教學專屬的容器並保留書籍資料（下次跑 `up` 可繼續用）。若要緊接著練習 **[交易傳播教學](transactions.mdx)**，可以先保留資料庫運行。

遇到埠口占用或 Token 失敗，請直接查 **[疑難排解](../reference/troubleshooting.md)**；完整端點與錯誤碼見 **[Bookstore HTTP API 參考](../reference/bookstore-api.md)**。
