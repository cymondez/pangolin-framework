---
title: 選擇版本與升級
description: 為應用固定套件來源，並在升級時檢查 API、設定與資料相容性。
---

目前 0.1.0 是預覽套件。頁首選單用來選擇套件對應的文件；有正式歷史版本後，你可以保留舊版指南繼續閱讀。

## 預覽期間如何固定依賴？

安裝頁使用本機 replace 指向套件來源。採用前請記錄該 checkout 的 commit，讓團隊使用同一份程式；不同的預覽來源可能包含 API 變更。

正式版本發布後，可以移除本機 replace 並使用對應的 Go module tag。當前還沒有正式 tag，請先依[安裝頁](../getting-started/installation.md)操作。

## 升級應用前

1. 選擇目標版本的文件，查看 API 與必要設定差異。
2. 在自己的測試環境更新依賴，重新建置並執行應用測試。
3. 使用資料庫或事件時，先確認資料表與事件 payload 相容。
4. 驗證主要使用情境與回復方式後，再部署應用。

套件版本與事件的 SchemaVersion 分別管理。升級框架不代表可以直接改寫已被其他服務使用的事件格式。

## 更新較早的 0.1.0 預覽來源

UOW sentinel 名稱已對齊 UOW_REQUIRED／UOW_CLOSED／UOW_BUSY，取代較早的 UOW_SCOPE_REQUIRED／UOW_SCOPE_ENDED／UOW_CONCURRENT_USE。已提交後的觀察器錯誤只記錄，不再使 Within 返回業務失敗。

Bookstore 的資料表新增 nullable deleted_at，app role 需要 UPDATE 權限；tenant policies 同時比對完整 Scope，Worker 也要更新 transaction-local 設定。不要只更新一半的 schema 或程序。保留資料時先備份並規劃明示的部署遷移；API 啟動不自動改表。

只使用 disposable demo 時，可先停止自己的 API／Worker，再確認 demo project 歸屬並清除其 volumes，然後重新執行教學準備步驟。這會刪除該 demo 的書籍資料，不能用在正式資料庫：

```powershell
$env:PANGOLIN_M1_PG_PORT = "15542"
$env:PANGOLIN_M1_NATS_PORT = "14322"
$env:PANGOLIN_M1_IDP_PORT = "18180"
docker compose -p pangolin-bookstore-demo -f deploy/compose/m1.yaml down --volumes --rmi local
pwsh ./examples/bookstore/scripts/demo.ps1 up
```

Runtime Options 新增 StartupChecks，請將 positional literal 改成具名欄位。使用可撤權的 Tenant Resolver／當前 Authority，Provider.Authorize 傳入 context 中相同的可信 binding。ScopeAware schema 升級、完整 inventory 與舊程序拒絕條件見[部署遷移](../data/migrations.md)；通用事件表與 Bookstore 薄片分開，需明示改接 Resource／Publisher／Worker。

未知 Scope Kind 在預設 Bookstore 由舊的 403 改為 400／SCOPE_KIND_UNSUPPORTED。組織範例改用獨立 ScopeAware Repository，不能直接沿用 TenantOnly 資料表。

## 更新 CLI 生成的專案

`pangolin new` 生成的專案內附固定版本的 `framework/` 與 `runtime-manifest.json`，以本機 module replacement 固定 Runtime。Runtime、pangolin CLI 與 Template 應來自同一個 Framework checkout／commit；升級時整套更新 Runtime 與 manifest，或從同一來源重新生成，不要混用不同來源。

替換後在生成專案執行 `go test ./...` 與 `pangolin doctor` 確認 checksum 與環境；Runtime 帶來新 migration 時，先執行 `go run ./cmd/migrate` 部署，再啟動 API。

### 安全升級工作流

使用與生成專案同一 CLI checkout 執行：

```text
pangolin upgrade plan
pangolin upgrade --dry-run
pangolin upgrade runtime
pangolin upgrade verify
```

`plan` 與 `--dry-run` 都不寫入。CLI 會顯示 Framework checkout 與框架內 `framework/VERSION` 的目前版本、目標 CLI 版本、可用時的 target build revision，以及逐檔三路分類：

- `unchanged`：base、local 與 target 相同
- `update`：local 等於 base、target 已變更
- `keep`：local 已包含 target，保留本地內容
- `add`：只有 target 有檔案
- `remove`：只有 base 有檔案且 local 未修改
- `conflict`：local 同時不等於 base 與 target，或使用者刪除了有差異的受管檔案
- `missing`：base 與 target 相同但 local 已缺檔

衝突只顯示檔案路徑與 base/local/target SHA-256 digest，不顯示檔案內容。dry-run 看見 conflict/missing 會回非零且保持工作樹零寫入；`runtime` 更會在 manifest 無效、路徑不安全或有 conflict/missing 時先失敗。

成功的 `runtime` 先 staging 再套用完整 target framework 與新 `runtime-manifest.json`，並保留 `.pangolin-backup/<UTC timestamp>/framework`、`runtime-manifest.json` 與 `pangolin.json`。指令輸出也會給出可直接在 PowerShell 執行的 rollback 指示；套用失敗會嘗試 restore，任何 rollback error 會併入原始錯誤。

`verify` 不寫入；它會確認 `pangolin.json` 已是 target version、目前 manifest 與 embedded target 一致、每個 framework checksum 正確，再執行 `go build -mod=readonly ./...`、`go test -mod=readonly ./...` 與 `go vet -mod=readonly ./...`。測試或 vet 注入失敗時，輸出會給出可執行的修正命令。

摘要中的 public Go API inventory、migration 與 settings/config counts 僅分類變動，不是語意相容性審查通過證明。DB migration、event schema 相容性、tenant/organization/profile switch、第三方服務相容與回復演練仍須人工 review。
### 組織 Profile 的切換限制

`tenant-only` 與 `organization` 是不同的依賴圖與 schema，不能只改 pangolin.json 切換。既有專案啟用組織能力需[前向遷移與移除檢查](../data/migrations.md)；已含組織資料的資料庫也不能直接接到 tenant-only 專案，生成 API 會核對資料庫 Profile，不合時拒絕 Ready。

升級前的停寫、備份、還原及 owner／RLS／待處理工作核對步驟見[備份、還原與升級檢查](../platform/backup-restore.md)。

## 加入背景工作與觀測

升級預覽來源後，部署 Jobs migration 與事件 CapacityMigrations，再更新 schema 相容版本範圍。事件 store 改用 NewBoundedStore 並設定容量；舊 NewStore 保留既有組裝相容性。完整[接線與上限](../platform/jobs.md)需要目前服務 grant 和 Handler 冪等。

Envelope 新增可省略 traceparent，舊格式仍可讀取，已發布 payload schema 不重寫。人工 replay 保留實際持久化的原始 Envelope，不重新生成 owner、事件 ID 或 traceparent。OpenTelemetry 採明確注入，應用負責 SDK 有界 Shutdown。

## 中立資料存取與登入擴充

一般 Repository 改用 persistence/database 的 Resource／Executor／Dialect；連線與 Driver 只留在組裝／Adapter。SQLite 的 Manager 明示 DefaultIsolation=Serializable，零值仍保留既有 ReadCommitted；缺少必要能力時用 database.Admit 拒絕組裝。既有引擎專用 SQL／migration 不會自動移植，見[資料庫選擇](../data/database.md)。

Settings 的空 Layer 位址保留為 Global；Config 是 WithConfiguration 提供的獨立唯讀來源。名稱及既有值不搬移，完整來源順序見[分層設定](../platform/settings.mdx)。Feature 先檢查 Permission，普通 Permission 不反向依賴 Feature。

原 authn.Identity／Validator／ServiceAuthenticator 保留相容轉接；新登入模組使用 security/identity 的 Verifier／Pipeline。HTTP 改用 AuthenticateCredential／Credentials，舊 Authenticate 或 Bookstore Auth 則保留 Bearer 入口，兩種模式擇一。Challenge／Pending 不安裝身分；Cookie 寫入需由 Session 模組提供 CSRF 接線，見[登入指南](../security/authentication.md)。
