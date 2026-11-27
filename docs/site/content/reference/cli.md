---
title: pangolin CLI 參考
description: pangolin CLI 全部子命令、module add --preset service、doctor --live、OpenAPI 生成與環境變數完整手冊。
---

> **前置操作**：若為第一次使用，請先跟跑一次 [用 CLI 建立自己的 Bookstore](../tutorials/generated-bookstore.md)。預覽版從 Framework checkout 建置 `./cmd/pangolin`；產品版本與生成 Runtime 均為 `0.1.0`。

以下用 `pangolin` 代表建置好的執行檔（PowerShell 教學以 `& $pangolin` 呼叫）。**除了 `new` 是在目標目錄的「上層」執行外，其餘所有命令皆在「生成專案的根目錄」執行。**

---

## 1. 常用命令總覽

| 命令 | 用途與實際產出 |
|---|---|
| `pangolin new bookstore --template modular-monolith` | 生成預設 `tenant-only` 獨立專案（含 `framework/`、Compose、migrator 與 API） |
| `pangolin new bookstore-org --template modular-monolith --with organization` | 生成 `organization-enabled` 專案（含完整 Enterprise／Organization 治理與 ScopeAware Schema） |
| `pangolin new bookstore --template modular-monolith --realtime` | 額外啟用 SignalR 即時通訊模組與 `REALTIME.md`（可與 `--with organization` 併用） |
| `pangolin module add catalog --depends persistence,authorization` | 生成精簡 Root 模組骨架並登記至 `UserRoot` 聚合 |
| `pangolin module add catalog --depends persistence,authorization --preset service` | 生成**可直接 `go test ./...` 通過的垂直切片**：application 契約／實作、型別化建構子注入、錯誤路徑、單元測試、README 與 registry 更新 |
| `pangolin migrate create add_books --module catalog` | 產生全專案唯一遞增序號的 Goose SQL 遷移骨架檔 |
| `pangolin migrate status` | 透過 `PANGOLIN_MIGRATION_DSN` 唯讀查詢目前資料庫 Goose Ledger 狀態 |
| `pangolin doctor` | 分層離線健檢：檢查專案檔案、runtime manifest 與 `go build ./...`；DB／Migration／Live API 明確標為 `SKIP`（不啟動 Compose） |
| `pangolin doctor --live` | 進階實機健檢：額外檢查 Compose 狀態、DB 連線、Goose Ledger 與 `/livez`／`/readyz`（有界逾時、不啟動服務、絕不回顯 DSN） |
| `pangolin generate openapi` | 依目前 Profile 輸出 OpenAPI 3.1 JSON 規格 |
| `pangolin upgrade plan` / `--dry-run` / `runtime` / `verify` | 三路比對並安全升級內附的 `framework/` Runtime（詳見[選擇版本與升級](upgrading.md)） |
| `pangolin --version` | 顯示目前 CLI 版本（`0.1.0`） |

### 命名、旗標與安全防護規則

- 每個命令皆支援 `--help`，錯誤皆回傳非零結束碼。
- **命名規則**：專案名稱以小寫字母開頭（允許小寫、數字、`_`、`-`）；模組與 migration 名稱僅允許小寫字母、數字與 `_`。名稱參數須放在 flags 之前，後續 flags 可任意順序排列。
- **嚴格驗證**：未知 flag、未知 preset 或 `--preset crud`（目前明示不支援）一律直接拒絕；無效的 `--depends` 會主動列出目前 Profile 合法可用的候選依賴名稱。
- **原子寫入與不覆寫使用者修改**：`module add` 先完整 staging 成功後才一次更新 registry 與 `pangolin.json`，失敗絕不留下殘缺目錄；`new` 拒絕既有目錄；重複 module／migration 拒絕；手動編輯過的 registry 不被覆寫；`generate openapi` 內容相同可重入，若有差異則保留原檔並回錯。

---

## 2. 生成專案的 Root 模組裝配架構

理解以下三個元件的分工，就能輕鬆在生成專案裡新增與串接業務模組：

```text
internal/app（應用根模組）   ◀── 僅負責 HTTP 入口、Drain Participant 與啟動相容檢查
   ├─ modules/registry.go (UserRoot)  ◀── 聚合你用 module add 新增的所有業務模組
   └─ foundation.Module               ◀── 平台基礎模組：擁有 DB/事件資源並 Export 框架契約
```

- `--depends persistence`（或 `authorization`／`organization`）會自動翻譯成 `module.Of[foundation.Module]()` 的編譯期 Type 相依。
- 你的業務建構子只需直接宣告需要的公開契約參數（例如 `func NewCatalogService(units uow.UnitOfWorkManager, scopes foundation.Scopes) *CatalogService`），並在 `Configure` 用 `module.AddSingleton(b.Services(), NewCatalogService)` 註冊即可。
- `bookstore` 只是生成專案內建的網域示範模組，不是你新模組應該依賴的平台層。

---

## 3. 遷移與環境變數清單

真正的資料庫部署請在生成專案執行 **`go run ./cmd/migrate`**（API 啟動永不自動套用 DDL；`--seed` 僅供明示初始化本機 demo）。

| 環境變數 | 適用對象 | 用途與預設值 |
|---|---|---|
| `PANGOLIN_MIGRATION_DSN` | `cmd/migrate`、`migrate status`、`doctor --live` | 具備 DDL 權限的部署連線字串（勿放 CLI 參數或 log） |
| `PANGOLIN_DSN` | API、`doctor --live` | 受 RLS 約束的最小權限應用帳號連線 |
| `PANGOLIN_AUTHORITY_DSN` | API | 查詢當前 Tenant／組織資格的 metadata 讀取帳號連線 |
| `PANGOLIN_ISSUER` / `PANGOLIN_JWKS_URL` | API | 可信 JWT issuer 與公鑰 JWKS 位址 |
| `PANGOLIN_LISTEN` | 原生 API | 監聽位址，預設 `127.0.0.1:18081` |
| `PANGOLIN_API_URL` | `doctor --live` | 指定健康檢查目標 API 位址（未設則依 `PANGOLIN_API_PORT`，預設 `18281`） |
| `PANGOLIN_PG_PORT` / `PANGOLIN_IDP_PORT` / `PANGOLIN_API_PORT` | 生成的 Docker Compose | 本機 loopback ports，預設 `15642`／`18280`／`18281` |

---

## 4. 與套件內 `examples/bookstore` 的 API 差異對照

| 比較項目 | CLI 生成的專案（本頁） | 框架內既有範例（[Bookstore API 參考](bookstore-api.md)） |
|---|---|---|
| `POST /books` 成功狀態 | **HTTP 200**（需帶 `Idempotency-Key`） | **HTTP 201** |
| 回應欄位 | `id`、`title` | `id`、`title`、`version` |
| 組織支援 | 生成時 `--with organization` 內建完整治理入口與 OpenAPI | 預設為 `tenant-only` 雙程序 API + NATS Worker 範例 |
