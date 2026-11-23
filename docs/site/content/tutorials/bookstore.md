---
title: Bookstore：建立有登入與資料庫的 API
description: 啟動 Bookstore、取得登入 Token、建立書籍並驗證租戶隔離。
---

這個教學會帶你跑一個真實 API，並實際使用身分驗證、資料庫交易、多租戶隔離與背景事件。

本頁使用**套件內既有的範例 API**：建立書籍回 HTTP 201，回應含 `version` 欄位。CLI 生成的 Bookstore 是另一種選擇：生成專案的建立與讀取都回 HTTP 200，欄位只有 `id`／`title`，見[用 CLI 建立自己的 Bookstore](generated-bookstore.md)。

完成後，你可以取得登入 Token、建立書籍，再用同一位使用者讀回。API 與背景 Worker 分開執行。

## 需要準備什麼？

- 已取得套件來源，並完成[第一個應用](../getting-started/quickstart.mdx)。
- Docker Desktop 或可用的 Docker Linux Engine，以及 Compose。
- PowerShell 7；本頁的環境設定與 HTTP 命令使用 PowerShell，Windows／macOS／Linux 可使用 `pwsh`。
- 空閒的 loopback ports：15542、14322、18180、18181。

以下 `$env:` 設定只對**目前終端機**有效；每個終端都要設定該段列出的值。

## 1. 準備示範服務

在套件來源的 **`pangolin-framework` 根目錄**執行：

```powershell
pwsh ./examples/bookstore/scripts/demo.ps1 up
```

腳本先檢查現有 Docker 環境，再為此教學建立自己的資料庫、NATS 事件服務與示範登入服務，並初始化書籍資料表。看到 `Bookstore services are ready` 後繼續。

這些服務只在本機使用。教學登入服務接受指定的使用者／租戶來產生 Token，不能用作正式登入系統；示範密碼也只適用於這個隔離環境。

## 2. 第一個終端：啟動 API

保持在套件來源根目錄，執行：

```powershell
$env:PANGOLIN_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_ISSUER = 'http://127.0.0.1:18180'
go run ./examples/bookstore/cmd/api -listen 127.0.0.1:18181
```

程序會持續執行；**保留此終端**，再開下一個。API 位於 `http://127.0.0.1:18181`。

## 3. 第二個終端：啟動背景 Worker

切到相同的套件來源根目錄：

```powershell
$env:PANGOLIN_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_NATS_URL = 'nats://127.0.0.1:14322'
$env:PANGOLIN_NATS_STREAM = 'BOOKSTORE_DEMO'
$env:PANGOLIN_NATS_SUBJECT = 'bookstore.demo.book_created'
$env:PANGOLIN_WORKER_TENANTS = 'tenant-a,tenant-b'
go run ./examples/bookstore/cmd/worker
```

同樣保留此終端。Worker 會將已提交的事件送往訊息服務，再更新查詢用資料。

## 4. 第三個終端：取得登入 Token

```powershell
$identity = @{ subject = 'alice'; tenant_id = 'tenant-a' } | ConvertTo-Json
$login = Invoke-RestMethod -Method Post -Uri http://127.0.0.1:18180/token `
  -ContentType application/json -Body $identity
$headers = @{ Authorization = "Bearer $($login.access_token)" }
```

alice 現在代表 tenant-a 的使用者。Token 有效期五分鐘；過期後重新執行這一步。

## 5. 建立並讀回一本書

**在同一個第三終端**繼續，保留前一步的 `$headers`：

```powershell
$bookId = [guid]::NewGuid().ToString()
$body = @{ id = $bookId; title = 'My first panGOlin book' } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:18181/books `
  -Headers $headers -ContentType application/json -Body $body
Invoke-RestMethod -Uri "http://127.0.0.1:18181/books/$bookId" -Headers $headers
```

應看見相同的 id、title 與 `version: 1`。建立為 HTTP 201、讀取為 200；API 會等資料庫確認提交才回應成功。

## 6. 試試跨租戶存取

繼續使用 alice 的 Token，但刻意要求另一個租戶：

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

預期輸出 `403`。請求 Header 是候選值，不能用它取得另一個租戶的權限。

## 7. 修改自己的業務

範例的 `examples/bookstore/application/books.go` 包含建立／讀取書籍的 Use Case；`domain/book.go` 定義書籍欄位與驗證；HTTP 路由位於 `transport/http/books.go`。

先嘗試修改書名驗證或回應內容，再理解[交易](../data/unit-of-work.md)、[PostgreSQL 存取](../data/postgresql.md)與[事件](../events/integration-events.mdx)的接法。[應用架構](../concepts/architecture.md)說明如何安排你自己的功能。

## 8. 結束教學

對 API 與 Worker 的終端各按 Ctrl+C，等程序結束。回到套件來源根目錄：

```powershell
pwsh ./examples/bookstore/scripts/demo.ps1 down
```

這會停止自己的教學服務，保留書籍資料。下次執行 `up` 即可繼續使用。啟動失敗或無法取得 Token 時，先看[疑難排解](../reference/troubleshooting.md)。
