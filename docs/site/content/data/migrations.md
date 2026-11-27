---
title: 部署資料庫遷移（Migrations）
description: 理解為什麼要將「部署期 DDL 遷移」與「API 啟動期相容性檢查」分離，並學會使用 Goose 與全域鎖安全管理多模組 Schema。
---

> **你在架構中的位置**：`CI/CD 部署階段` **`[ 獨立部署帳號執行 runner.Apply（含全域鎖）]`** `──▶ API 啟動階段` **`[ 唯讀檢查 runner.CheckCompatibility ]`** `──▶ Module.Start`
>
> **前置建議**：已準備 PostgreSQL 連線，並了解應用的[啟動檢查（StartupChecks）](../fundamentals/application-startup.mdx)。

## 為什麼禁止 API 啟動時「順便自動跑 Migration」？

很多小型框架喜歡在 API 啟動時自動執行 `ALTER TABLE`。在多副本（Multiple Replicas）與多租戶生產環境裡，這種做法隱藏三個致命風險：

1. **過度授權**：API 每天直接面對外部流量，如果它的 DB 帳號握有 `CREATE/ALTER/DROP TABLE` 權限，一旦出現漏洞風險極大；而且 table owner 在 PostgreSQL 預設會繞過 RLS！
2. **多副本搶跑 DDL**：K8s 同時起 5 個 Pod，5 個程序同時跑 Migration 造成鎖死或版本表損毀。
3. **版本不相容卻硬啟動**：新舊版程式同時上線時，如果 Schema 已經升級或還沒升級，沒有版本區間檢查的 API 會在執行一半才噴 SQL 錯誤。

panGOlin 強制將責任拆成兩個獨立階段：
- **部署程序（Migration Role）**：持有 DDL 權限，透過 `runner.Apply` 取得 PostgreSQL 全域 Session Advisory Lock，依模組相依順序序列化套用 SQL Migration。
- **API 程序（App Role，最小權限）**：完全不碰 DDL，只在啟動前的 `StartupChecks` 執行 `runner.CheckCompatibility`，確認目前資料庫版本落在此版 API 允許的 `[minimum, maximum]` 區間內，否則拒絕進入 Ready。

---

## 1. 框架內建 Migration 來源一覽

panGOlin 各模組已透過 Go `embed` 提供版本化 SQL Migration。多數檔案位於 `migrations` 子目錄（需先以 `fs.Sub` 取出）；`organization.Migrations()` 則已直接回傳就緒的 `fs.FS`：

| 啟用的框架功能 | Migration 來源 | 備註 |
|---|---|---|
| 本地 Tenant 資格投影 | `tenancy/postgres.Migrations` | `migrations` 子目錄 |
| 必要安全稽核（Audit） | `platform/audit.Migrations` | `migrations` 子目錄 |
| 中立 Owner Catalog（`resource_scopes`） | `migration.ScopeMigrations` | 啟用 ScopeAware 資料表時需要 |
| 可選組織治理（Enterprise／Organization） | `organization.Migrations()` | **Tenant-only 模式不要載入** |
| 可靠事件（Outbox／Inbox） | `events/postgres.Migrations` | `migrations` 子目錄 |
| 事件容量配額（Bounded Capacity） | `events/postgres.CapacityMigrations` | 子目錄為 **`capacity_migrations`** |
| Command 冪等 | `idempotency.Migrations` | `migrations` 子目錄 |
| 持久化背景工作（Jobs） | `platform/jobs/postgres.Migrations` | `migrations` 子目錄 |

使用 `migration.JoinFS` 把所有需要的功能與你自己的業務 Migration 掛到各自目錄，再用 `migration.ModuleMigrations{Module, Directory, DependsOn}` 宣告模組間的 Schema 相依順序。

> **啟動前自動防呆**：版本號在整個應用內必須唯一，且被依賴模組的版本號必須早於使用者。缺依賴、循環、重複版本或順序倒置都會在執行任何 DDL 之前就被拒絕。

---

## 2. 完整部署程式範例：`setup/deployment.go`

以下完整檔案可放進你自己專案的 `setup/deployment.go`。`minimum` 與 `maximum` 請依你這個版本的應用實際相容的 Schema 版本區間填寫，不要直接照抄其他專案的數字：

```go
package setup

import (
	"context"
	"database/sql"
	"io/fs"

	"github.com/pangolin-framework/pangolin/persistence/migration"
	_ "github.com/jackc/pgx/v5/stdlib" // 註冊 pgx 給標準 database/sql（Goose 使用）
)

// Deploy 供 CI/CD 或獨立部署指令呼叫：取得全域鎖、依相依順序套用 SQL Migration，並驗證最終相容性。
// 注意：傳入的 db 須使用具備 DDL 權限的部署專用帳號，且連線池 MaxOpenConns 至少須 >= 2。
func Deploy(
	ctx context.Context,
	db *sql.DB,
	source fs.FS,
	modules []migration.ModuleMigrations,
	minimum, maximum int64,
) error {
	// 步驟 1：建立 PostgreSQL Migration Runner，綁定合併後的 fs.FS 與可接受版本區間 [minimum, maximum]。
	runner, err := migration.NewPostgres(db, source, minimum, maximum)
	if err != nil {
		return err
	}
	// 步驟 2：在全域 PostgreSQL Session Advisory Lock 保護下依模組相依順序執行 Goose 遷移。
	// 若同時有兩個部署程序啟動，第二個會安全排隊序列化，不會互撞。
	if err = runner.Apply(ctx, modules); err != nil {
		return err
	}
	// 步驟 3：遷移完成後立刻核對版本區間，確保資料庫狀態符合本版應用預期。
	return runner.CheckCompatibility(ctx)
}
```

### 執行注意事項
- 傳入 `Deploy` 的 `*sql.DB` 至少需允許 **2 條連線**（一條維持全域 Advisory Lock，一條執行 Goose DDL），並由呼叫者在程式結束時負責 `Close()`。
- 即使部署中途被取消（`ctx` 取消），Runner 仍會用獨立有界的清理 context 釋放鎖；若無法確認解鎖成功，該條連線會直接丟棄讓資料庫自動釋放 Session Lock。

---

## 3. 在 API 啟動時掛接相容性檢查

API 程序本身**不呼叫 `Apply`**，而是用唯讀查詢權限建立 Runner，把 `runner.CheckCompatibility` 掛到 `app.StartupChecks(...)`（或 `runtime.Options.StartupChecks`）：
- 檢查會在 **DI 容器 Seal 之後、任何 `Module.Start` 之前** 執行。
- 如果資料庫還沒跑 Migration 或版本過舊／過新，API 會在第 0 秒明確報錯並拒絕進入 Ready。

### 更換 Scope Provider 或移除組織模組時的額外檢查
若專案有啟用資源 Scope，啟動檢查應一併使用 `postgres.ScopeInventory`（透過可讀取完整 metadata 的受信任連線，**不能用受 RLS 過濾的一般 app Pool**）呼叫 `registry.CheckInventory`，確認中立 `resource_scopes`、業務資料表、未完成事件與未完成 Job 中沒有殘留已移除的 Scope Kind。

---

## 4. 從 Tenant-only 升級為 ScopeAware（組織隔離）的安全步驟

若既有線上資料庫已有業務資料，要從預設的 Tenant-only 升級到支援 Enterprise／Organization 的 ScopeAware Schema，請嚴格按以下順序執行：

1. **備份並實測還原**：先依[備份、還原與升級檢查](../platform/backup-restore.md)建立完整備份，並在隔離空庫演練還原。
2. **停寫與 Drain**：停止 API 接入與 Worker 認領，完成 Drain 與對帳。
3. **明確回填與原子替換 RLS**：
   - 把既有業務資料明確回填為 `(TenantID, "tenant", TenantID)`。
   - 建立含完整 owner 的複合唯一鍵與複合外鍵，參照中立 `resource_scopes` catalog。
   - **在同一筆交易內移除舊的 permissive RLS Policy 並安裝新的精確 owner Policy**（若留著舊 Policy，PostgreSQL 會把多個 permissive policy 用 `OR` 合併，導致權限被意外放寬！）。
4. **更新應用程序**：Bookstore 提供可選的 `infrastructure.ScopeUpgrade` 示範檔，會將 `books` 升級為 `scope_books` 以搭配 `ScopedRepository`；舊的 tenant-only API 相容性檢查會自動拒絕新版 Schema，防止混跑。

---

## 遷移黃金法則

- **永遠向前修復（Forward-only）**：已套用到線上的 Migration 檔絕不事後修改；需要修正時一律新增下一個版本號的 Migration。
- **不用 `allow-missing` 掩蓋亂序**：亂序遷移往往代表團隊分支合併衝突，必須在部署前修正版本編號。

## 下一步

資料層核心已經全部貫通！接下來進入安全與多租戶防線：從 **[可擴充登入與身分驗證](../security/authentication.md)** 開始，一路打通 Tenant、授權、資源 Scope 與可靠事件。
