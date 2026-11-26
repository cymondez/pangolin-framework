---
title: 用 CLI 建立自己的 Bookstore
description: 生成獨立 Go 專案，透過 Docker Compose 啟動有登入、交易與租戶隔離的 API。
---

完成這頁後，你會有自己的 Bookstore 專案，能登入、建立書籍、讀回資料，並重送同一個請求。

## 開始前

準備 Go 1.27.4、Git、Docker Linux Engine／Compose，以及 PowerShell 7。以下命令使用 Windows PowerShell 7。確認 15642、18280、18281 沒有被其他服務使用。預覽版先從 Framework checkout 建置 CLI，不需要 Node.js 或 Python。

## 1. 建置 CLI，建立獨立專案

在取得的 **pangolin-framework 根目錄**執行：

```powershell
go build -o .cache/bin/pangolin.exe ./cmd/pangolin
$pangolin = (Resolve-Path .cache/bin/pangolin.exe).Path
cd ..
& $pangolin new bookstore --template modular-monolith
cd bookstore
go test ./...
```

預期看到 `Created bookstore profile tenant-only runtime 0.1.0`，以及通過的 domain 測試。現在的工作目錄是你自己的 **bookstore**，與 Framework checkout 並排；後續命令都在這裡執行。

生成物包含固定版本的 `framework/` 與本機 module replacement，可在預覽版尚未發布正式 tag 時直接建置。不要只換掉其中一部分檔案；升級時一起更新 Runtime 與 checksum manifest。

## 2. 啟動服務

```powershell
docker compose up -d --build --wait
(Invoke-WebRequest http://localhost:18281/readyz).StatusCode
```

預期輸出 `204`。首次建置需要下載固定版本的 Go 依賴。若尚未 Ready，先查看 `docker compose logs api migrate`，確認部署 migration 完成，再繼續。

Compose 會啟動 PostgreSQL、本機示範登入服務、一次性的 migration 程序與 API。應用使用受 RLS 約束的角色；部署 migration 使用另一組連線。示範帳號與密碼只供本機學習，正式環境換成自己的可信登入來源與環境 Secrets。

## 3. 取得 Token，建立並讀回書籍

```powershell
$login = Invoke-RestMethod -Method Post http://localhost:18280/token `
  -ContentType application/json -Body '{"subject":"alice","tenant_id":"tenant-a"}'
$headers = @{
  Authorization = "Bearer $($login.access_token)"
  'Idempotency-Key' = 'first-book'
}
$body = '{"id":"book-1","title":"My first generated app"}'
Invoke-RestMethod -Method Post http://localhost:18281/books `
  -Headers $headers -ContentType application/json -Body $body
Invoke-RestMethod http://localhost:18281/books/book-1 -Headers $headers
```

兩次回應都有 `id: book-1` 與相同 title。生成 API 的建立與讀取皆回 HTTP 200；寫入必須等確認 Commit 後才回應。Token 有效期五分鐘，過期後重做登入。

再執行一次同一個 POST，保留原本的 key 與 body，會重播原結果。改變 body 卻沿用 `first-book`，預期回 409。Idempotency key 包含目前使用者、Tenant 與完整 Scope，不會與其他 owner 的相同 key 混在一起。

API 會同交易保存書籍、必要稽核、成功結果與 Outbox 事件。若要把事件配送到 Broker，另依[可靠配送指南](../events/reliable-delivery.md)組裝 Worker；這份生成 Compose 先提供可讀寫 API 與持久化 Outbox。

## 4. 需要 Organization 時

先完成預設範本，再回到 bookstore 的上層目錄，建立另一個專案：

```powershell
cd ..
& $pangolin new bookstore-org --template modular-monolith --with organization
cd bookstore-org
go test ./...
```

這個選項一次加入完整的 Enterprise、Organization、治理入口、migration 與 Scope-aware Bookstore。預設 tenant-only 不包含組織實作、資料表或路由。

先停止前一個專案的 Compose，或為這個專案設定另一組 ports，再啟動它：

```powershell
$env:PANGOLIN_PG_PORT = '15643'
$env:PANGOLIN_IDP_PORT = '18290'
$env:PANGOLIN_API_PORT = '18291'
docker compose up -d --build --wait
```

以 `http://localhost:18290/token` 登入 alice／tenant-a，再呼叫 18291 的 API。指定 `X-Scope-Kind: org.organization`、`X-Scope-ID: 00000000-0000-4000-8000-000000000002` 即選擇示範 Org A；Org B 的 ID 結尾為 003。兩個 Org 可各自建立相同 book ID，資料互不共用。更多治理用法見[組織管理](../security/organizations.md)。

既有應用啟用組織能力需要前向遷移；不能只改 pangolin.json。生成 API 會核對資料庫 Profile，不能把組織資料庫直接接到 tenant-only 專案。

## 5. 新增自己的模組

保持在自己的生成專案：

```powershell
& $pangolin module add catalog --depends persistence,authorization
& $pangolin migrate create add_catalog --module catalog
& $pangolin generate openapi
& $pangolin doctor
```

catalog 會以 Root 模式加入 UserRoot 聚合，並附上 module.go 與 README。在 `Configure` 內用 `b.Services().Add(NewCatalogService)` 註冊建構子（預設 Transient，需要時加 `module.Singleton()`），在 `Start` 內用 `rt.Services()` 解析精確型別。框架能力以型別直接注入：`--depends persistence`／`authorization`（或 `organization`）翻譯為 `foundation` 的 Type 邊，建構子參數直接寫 `uow.UnitOfWorkManager`、`foundation.Scopes`、`foundation.Publisher`、`foundation.Idempotency` 等公開契約；`foundation` 平台模組取得所有 DB／事件資源，`internal/app` 根模組只保留 HTTP 組裝與相容檢查。`bookstore` 名稱是生成網域示例，請依賴 foundation 能力或已註冊的使用者模組。填寫 migration SQL 後，先部署 migration 再啟動使用新 schema 的 API。命令說明與環境變數見 [CLI 參考](../reference/cli.md)。

## 停止與下一步

在各生成專案內執行 `docker compose down --volumes --rmi local`，只移除該專案的本機示範環境。資料會隨專用 volume 刪除。

接著閱讀[分層設定與 Features](../platform/settings.mdx)，把使用者偏好、產品配額與快取加入自己的服務。若想觀察完整 API／Broker／Worker 的分程序流程，繼續使用[套件內的 Bookstore 教學](bookstore.md)。
