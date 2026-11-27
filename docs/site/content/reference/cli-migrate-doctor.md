---
title: 遷移與健檢：pangolin migrate 與 pangolin doctor
description: 完整說明 pangolin migrate create、migrate status、部署用 go run ./cmd/migrate，以及 doctor 離線檢查與 doctor --live 實機健檢項目與解讀方式。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

這一頁說明 CLI 生成專案的兩大維運指令群：
1. **資料庫遷移工作流**：`pangolin migrate create`、`go run ./cmd/migrate`、`pangolin migrate status`。
2. **分層健康檢查**：`pangolin doctor`（離線靜態檢查）與 `pangolin doctor --live`（實機環境檢查）。

> **工作目錄提醒**：本頁所有指令一律在**生成專案根目錄**執行。

---

## 1. 建立遷移檔：`pangolin migrate create`

```text
pangolin migrate create NAME --module MODULE
```

| 參數／旗標 | 必填 | 說明 |
|---|---|---|
| `NAME` | 是（須在 flags 之前） | 遷移描述名稱，符合 `^[a-z][a-z0-9_]{0,47}$`（不可為 Go 保留字或 `main`） |
| `--module` | 是 | 已登記於 `pangolin.json` 的合法模組名稱 |

### 運作細節

- CLI 會掃描整個 `modules/` 目錄下既有 `.sql` 檔的前綴版本號，從全專案基準 **`5000000`** 往上取唯一單調遞增序號（第一支使用者遷移檔即為 `5000001_<name>.sql`），確保跨模組版本號全局唯一。
- 檔案建立於 `modules/<module>/migrations/<seq>_<name>.sql`，預設內容為前向安全 Goose 範本：
  ```sql
  -- +goose Up
  -- Add forward-only SQL, owner constraints, grants and FORCE RLS here.

  -- +goose Down
  SELECT 1;
  ```
- 若同一模組中已存在結尾為 `_<name>.sql` 的檔案，CLI 會直接拒絕（`migration already exists`）。

---

## 2. 實際部署資料庫遷移：`go run ./cmd/migrate`

> **關鍵安全邊界**：`pangolin` CLI 本身**不會**對資料庫執行 DDL；生成專案的 API 在啟動時也**只做唯讀相容性檢查（`CheckCompatibility`）**，永不自動跑 Migration。真正套用 Schema 變更請透過生成專案內建的部署入口 `cmd/migrate`。

`go run ./cmd/migrate` 會讀取環境變數 `PANGOLIN_MIGRATION_DSN`，在 PostgreSQL 全域 Session Advisory Lock 下依模組相依順序執行：
1. 內建平台遷移：`tenant`、`audit`、`scope`、`events`、`capacity`、`idempotency`、`bookstore` 與目前 Profile 遷移（含 `organization`）。
2. 所有在 `modules/<name>/migrations/*.sql` 內的新增使用者遷移（依 `pangolin.json` 的模組相依順序排列）。
3. 冪等地配置有界事件佇列配額（`ProvisionQueues`）。
4. 若傳入 `--seed` 旗標，則額外寫入**僅供本機 Disposable Demo 使用**的示範成員（`alice`）與 scope 種子資料（**正式環境請勿加 `--seed`**）。

---

## 3. 查詢資料庫遷移狀態：`pangolin migrate status`

```text
pangolin migrate status
```

- 透過 `PANGOLIN_MIGRATION_DSN` 唯讀查詢 `pangolin.goose_db_version` 表並按 `id` 排序印出每一筆已套用版本：
  ```text
  1000001 applied=true
  ...
  5000000 applied=true
  5000001 applied=true
  ```
- 若尚未執行過部署遷移，會回傳 `migration status unavailable; apply deployment migrations first`。

### 雙環境遷移操作範例

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 1. 為 catalog 模組建立新遷移骨架檔
& $pangolin migrate create add_catalog --module catalog

# （編輯 modules/catalog/migrations/5000001_add_catalog.sql 加入資料表、授權與 FORCE RLS）

# 2. 使用具備 DDL 權限的部署連線套用遷移（本機示範可選擇加 --seed）
$env:PANGOLIN_MIGRATION_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15642/pangolin_m0?sslmode=disable'
go run ./cmd/migrate

# 3. 唯讀查詢 Goose Ledger 確認所有版本皆為 applied=true
& $pangolin migrate status
Remove-Item Env:PANGOLIN_MIGRATION_DSN
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 1. 為 catalog 模組建立新遷移骨架檔
"$PANGOLIN" migrate create add_catalog --module catalog

# （編輯 modules/catalog/migrations/5000001_add_catalog.sql 加入資料表、授權與 FORCE RLS）

# 2. 使用具備 DDL 權限的部署連線套用遷移（本機示範可選擇加 --seed）
export PANGOLIN_MIGRATION_DSN='postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15642/pangolin_m0?sslmode=disable'
go run ./cmd/migrate

# 3. 唯讀查詢 Goose Ledger 確認所有版本皆為 applied=true
"$PANGOLIN" migrate status
unset PANGOLIN_MIGRATION_DSN
```

</TabItem>
</Tabs>

---

## 4. 離線與實機健檢：`pangolin doctor` 與 `pangolin doctor --live`

```text
pangolin doctor [--live]
```

`doctor` 嚴格區分「離線靜態檢查」與「實機環境檢查」，兩者都**只做唯讀驗證、絕不自動啟動 Docker Compose，也絕不回顯 DSN 機密內容**。

### 離線健檢（預設：`pangolin doctor`）

依序檢查下列項目（逾時上限 2 分鐘）：

| 輸出項目 | 檢查內容 | 失敗時常見原因 |
|---|---|---|
| `[PASS] project files` | `pangolin.json` 合法且版本與 CLI 相容、`go.mod` 與 `modules/registry.go` 均為一般檔案 | 不在專案根目錄，或關鍵專案檔案遺失／變成符號連結 |
| `[PASS] runtime manifest` | `framework/go.mod` 存在，且 `runtime-manifest.json` 內每一個受管檔案的 SHA-256 皆與本地 `framework/` 一致 | 手動修改了 `framework/` 內檔案，或 `runtime-manifest.json` 損毀 |
| `[PASS] go build ./...` | 以 `go build -mod=readonly ./...` 完整編譯整個專案，並驗證 `go.mod` 與 `go.sum` 在編譯前後位元組完全未變動 | 程式碼編譯錯誤，或 `go.mod`／`go.sum` 尚未 tidy |
| `[SKIP] database, requires --live` | 明確告知離線模式略過資料庫連線檢查 | — |
| `[SKIP] migrations, requires --live` | 明確告知離線模式略過遷移 Ledger 檢查 | — |

### 實機健檢（`pangolin doctor --live`）

先通過全部離線靜態檢查，再逐項執行以下 5 項實機檢查（所有檢查皆設 3–5 秒有界逾時，全部項目皆會跑完並列出逐項結果）：

| 輸出項目 | 實際檢查動作 | 所需環境變數／前置 |
|---|---|---|
| `[PASS] compose state` | 執行 `docker compose ps --format json`，確認專案內 `api` 服務處於 `running` 狀態 | Docker Compose 服務已透過 `docker compose up -d` 啟動 |
| `[PASS] database connection` | 以 `pgx` 連線並 `Ping` 資料庫（5 秒逾時） | `PANGOLIN_DSN`（最小權限應用帳號） |
| `[PASS] migration ledger` | 查詢 `pangolin.goose_db_version`，確認至少有一筆 `is_applied = true` | `PANGOLIN_MIGRATION_DSN` |
| `[PASS] API /livez` | `GET <API_URL>/livez` 回傳 HTTP `2xx`（3 秒逾時） | `PANGOLIN_API_URL`（未設則用 `http://127.0.0.1:${PANGOLIN_API_PORT:-18281}`） |
| `[PASS] API /readyz` | `GET <API_URL>/readyz` 回傳 HTTP `2xx`（3 秒逾時） | 同上 |

全部通過後結尾印出 `[PASS] doctor --live`；若有任一失敗則印出對應 `[FAIL]` 行並回傳 `doctor --live completed with failures`（非零結束碼）。

### 雙環境健檢操作範例

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 1. 離線靜態健檢（不需啟動 Docker 或設定 DSN）
& $pangolin doctor

# 2. 啟動服務後執行實機健檢
$env:PANGOLIN_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:15642/pangolin_m0?sslmode=disable'
$env:PANGOLIN_MIGRATION_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15642/pangolin_m0?sslmode=disable'
$env:PANGOLIN_API_URL = 'http://127.0.0.1:18281'
& $pangolin doctor --live

# 清除目前視窗的示範環境變數
Remove-Item Env:PANGOLIN_DSN, Env:PANGOLIN_MIGRATION_DSN, Env:PANGOLIN_API_URL
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 1. 離線靜態健檢（不需啟動 Docker 或設定 DSN）
"$PANGOLIN" doctor

# 2. 啟動服務後執行實機健檢
PANGOLIN_DSN='postgres://pangolin_app:local-app-only@127.0.0.1:15642/pangolin_m0?sslmode=disable' \
PANGOLIN_MIGRATION_DSN='postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15642/pangolin_m0?sslmode=disable' \
PANGOLIN_API_URL='http://127.0.0.1:18281' \
  "$PANGOLIN" doctor --live
```

</TabItem>
</Tabs>

### 預期輸出（`doctor --live` 全綠）

```text
[PASS] project files
[PASS] runtime manifest
[PASS] go build ./...
[PASS] compose state
[PASS] database connection
[PASS] migration ledger
[PASS] API /livez
[PASS] API /readyz
[PASS] doctor --live
```

---

## 5. 常見錯誤與排查

| 錯誤訊息 | 原因 | 處置方式 |
|---|---|---|
| `registered --module required` | `--module` 未填或該模組不在 `pangolin.json` 中 | 先用 `pangolin module add` 註冊模組，或確認模組拼字 |
| `PANGOLIN_MIGRATION_DSN required` | 執行 `migrate status` 未設定 `PANGOLIN_MIGRATION_DSN` | 在目前終端設定 `PANGOLIN_MIGRATION_DSN` 後重試 |
| `[FAIL] runtime manifest: bundled runtime checksum mismatch` | `framework/` 下有檔案被手動改動 | 還原 `framework/` 修改；切勿直接編輯受管 Runtime，升級請走 `pangolin upgrade` |
| `[FAIL] compose state: Docker Compose is unavailable or no running project service returned` | Compose 尚未啟動，或 `api` 容器已退出 | 執行 `docker compose ps` 與 `docker compose logs api migrate` 排查 |
| `[FAIL] API /readyz: HTTP 503` | API 存活但資料庫連線或啟動相容性尚未就緒 | 確認 PostgreSQL 健康與 `go run ./cmd/migrate` 已完成 |

## 下一步

繼續了解如何輸出 OpenAPI 規格與安全升級內附 Runtime：前往 **[規格與升級：`pangolin generate openapi` 與 `pangolin upgrade`](cli-openapi-upgrade.md)**。
