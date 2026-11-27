---
title: pangolin CLI
description: 生成兩種 Bookstore Profile、新增模組與 migration、查驗環境並產生 OpenAPI。
---

先完成[生成 Bookstore 教學](../tutorials/generated-bookstore.md)。預覽版從 Framework checkout 建置 cmd/pangolin；產品版本與生成 Runtime 都是 0.1.0。

## 命令

以下以 pangolin 代表你建置的可執行檔；PowerShell 教學使用 `& $pangolin`。除 new 在目標目錄的上層執行，其餘命令都從生成專案根目錄執行。

| 命令 | 結果 |
|---|---|
| `pangolin new bookstore --template modular-monolith` | 建立預設 tenant-only 專案 |
| `pangolin new bookstore-org --template modular-monolith --with organization` | 同時加入完整 Enterprise／Organization 與 Scope-aware schema |
| `pangolin module add catalog --depends persistence,authorization` | 產生預設 Root skeleton（`ModuleMetadata`／`DependsOn`／`RootBuilder` hooks），framework 名稱翻譯為 `foundation` Type 邊並登記 UserRoot 聚合 |
| `pangolin module add catalog --depends persistence,authorization --preset service` | 產生可直接 build/test 的 service vertical slice：application contract/implementation、typed constructor injection、失敗路徑、單元測試、README 與 registry 更新；旗標可任意順序 |
| `pangolin migrate create add_books --module catalog` | 產生全專案唯一序號的 Goose SQL 骨架 |
| `pangolin migrate status` | 查詢實際資料庫的 Goose ledger |
| `pangolin doctor` | 分層檢查專案檔案、runtime manifest 與 `go build ./...`；DB、migration ledger 與 live API 明示為 SKIP |
| `pangolin doctor --live` | 額外檢查 Compose state、DB 連線、Goose ledger 與 `/livez` `/readyz`；不啟動服務，逾時有界且不回顯 DSN |
| `pangolin generate openapi` | 輸出目前 Profile 的 OpenAPI 3.1 JSON |
| `pangolin --version` | 顯示 0.1.0 |

每個命令接受 --help，錯誤以非零退出。專案名稱以小寫字母開頭，可用小寫字母、數字、底線與連字號；模組與 migration 名稱使用小寫字母、數字及底線。名稱參數放在 flags 之前；依 flags 可出現在 name 後的任意順序。未知 flag 或未知 preset 都會拒絕。`--preset crud` 目前明確為 unsupported，不會產生半套 CRUD。模組依賴必須已存在且無循環；無效依賴會列出依據目前 profile／registered modules 可用的 candidates。

`module add --preset service` 的第一個驗證是：

```text
cd <generated-project>
go test ./...
```

成功的 module 會一次性 staging，完成後更新 registry 與 `pangolin.json`；失敗不保留只寫一半的 module 目錄。

## 既有檔案與重複執行

new 拒絕已存在的目標目錄。重複 module／migration 拒絕；手動編輯過的 registry 不被覆寫。generate openapi 遇到相同內容可重入，遇到不同內容則保留原檔並回錯誤。doctor 可重複執行，不啟動服務。

生成 app 的入口走 Root 裝配：`modules/registry.go` 產出 user-modules 的 `UserRoot` 聚合（Type 相依閉環、菱形去重），`foundation` 套件的 `foundation.Module` 平台模組擁有 DB／事件資源與公開框架契約（`foundation.UnitOfWork`、`foundation.Scopes`、`foundation.Registry`、`foundation.Publisher`、`foundation.Idempotency` 等），`StartRoot` 之後才在 seal 內取得任何資源。`module add --depends persistence`（或 `authorization`／`organization`）把 framework 名稱翻譯成 `module.Of[foundation.Module]()` Type 邊；你的建構子直接以型別取得框架服務，例如 `func NewCatalogService(units uow.UnitOfWorkManager, scopes foundation.Scopes) *CatalogService`，再以 `b.Services().Add(NewCatalogService)` 註冊。不需要 app root 匯入：根模組僅保留 HTTP／drain 組裝與相容檢查；`bookstore` 名稱是生成網域示例，不是 user module 依賴。

## 遷移與環境

migrate status 從環境變數 PANGOLIN_MIGRATION_DSN 取得連線，僅讀取狀態。真正部署在生成專案執行 `go run ./cmd/migrate`；完成 SQL 後才執行，應用 API 不會代你套用 DDL。

| 環境變數 | 用途 |
|---|---|
| PANGOLIN_MIGRATION_DSN | 部署／status 的資料庫連線 |
| PANGOLIN_DSN | API 的受限制應用角色 |
| PANGOLIN_AUTHORITY_DSN | 目前 Tenant／組織資格的 metadata 讀取角色 |
| PANGOLIN_ISSUER／PANGOLIN_JWKS_URL | 可信 JWT issuer／金鑰來源 |
| PANGOLIN_LISTEN | 原生 API 監聽位址；預設 127.0.0.1:18081 |
| PANGOLIN_PG_PORT／PANGOLIN_IDP_PORT／PANGOLIN_API_PORT | 生成 Compose 的 loopback ports；預設 15642／18280／18281 |

`--seed` 僅供明示初始化本機 demo。DSN 與 Token 不放在命令參數或 log。缺少 ledger 時 status 回錯誤，先部署 migration；API 發現 schema／Profile 不合時拒絕 Ready。`doctor --live` 使用 `PANGOLIN_DSN` 與 `PANGOLIN_MIGRATION_DSN`，API base 可由 `PANGOLIN_API_URL` 指定，否則使用 `PANGOLIN_API_PORT`（預設 `18281`）。只讀或查詢失敗會逐項回報，不啟動 Compose 或套用 migration，不 dump DSN／secret。

## 生成 API 的範圍

生成物提供 `POST /books` 與 `GET /books/{id}`，欄位為 id／title；建立需要 Idempotency-Key，回應 HTTP 200。Organization Profile 另提供完整治理入口。OpenAPI JSON 包含對應路由、schema、Bearer authentication、Scope hints 與 Problem Details。

內附 Runtime bundle 與 runtime-manifest.json，用相對 replace 在首版發行前固定來源。tenant-only 的依賴圖不含組織實作；--with organization 不提供半套 Enterprise 選項。既有專案切換能力需[前向遷移與移除檢查](../data/migrations.md)，不是修改一個設定值。

若要學習套件內原有 API／Worker 與 Broker 範例，使用 [Bookstore API 參考](bookstore-api.md)。那個範例的建立回 HTTP 201 且回應另含 `version` 欄位；這裡生成的 API 建立與讀取都回 200、欄位只有 `id`／`title`，兩者分開說明。
