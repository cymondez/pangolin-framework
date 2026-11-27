---
title: 備份、還原與升級檢查（Disaster Recovery & Upgrade Runbook）
description: 使用 PostgreSQL 原生工具建立可驗證備份、在隔離空資料庫還原並核對 owner、RLS 與待處理工作，再安全部署相容版本。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

> **你在架構中的位置**：`正式維運與升級防線` **`[ 停寫 Drain → pg_dump + SHA-256 → 新空庫 pg_restore → 獨立核對 Owner/RLS/佇列 → 同源升級與恢復 ]`**
>
> **適用環境**：以下命令同時提供 **Windows PowerShell** 與 **Linux / macOS (Bash)** 版本，請於核准的維運主機與受保護工作目錄執行。

## 為什麼「有跑 `pg_dump`」不等於「備份可還原」？

對多租戶 panGOlin 應用而言，還原資料庫不是只有資料列數對就好，還必須確認三件最容易在災難復原時出錯的事：
1. **全域角色與 RLS 權限是否完整**：`pg_dump` 只備份單一資料庫，**不包含** PostgreSQL 全域角色（`app`、`worker`、`migrator`）！若還原時誤用 superuser 或 table owner 當成應用帳號，RLS 隔離會瞬間失效。
2. **Owner 與待處理工作是否完整保留**：中立 `resource_scopes` catalog、Organization membership，以及尚未送完的 Outbox／Inbox／Jobs 紀錄都必須保留原 owner。
3. **Runtime、CLI、Schema Profile 是否同源相容**：不可把帶有組織資料的資料庫直接接給 `tenant-only` API。

---

## 開始前檢查清單

1. **記錄版本基線**：在應用部署目錄確認目前產品版本、Framework 來源 commit、Profile（`tenant-only` 或 `organization`）、Migration 版本與事件 Schema 版本。
2. **安全注入連線字串**：備份用 `SOURCE_DSN`、還原用管理連線 `ADMIN_DSN`（皆須為 `postgres://` 或 `postgresql://` URI）。**絕不把含密碼的 URI 寫進命令列參數、文件或 shell history**；正式環境建議採無密碼 URI 搭配受保護的 `PGPASSFILE` 或部署密鑰檔。
3. **確認用戶端版本**：先確認本機 `pg_dump`、`pg_restore` 與 `createdb` 版本相容於目標 PostgreSQL Server，並在任一命令失敗時立即中止：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
pg_dump --version
if ($LASTEXITCODE -ne 0) { throw 'pg_dump --version failed' }
pg_restore --version
if ($LASTEXITCODE -ne 0) { throw 'pg_restore --version failed' }
createdb --version
if ($LASTEXITCODE -ne 0) { throw 'createdb --version failed' }
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
set -euo pipefail
pg_dump --version
pg_restore --version
createdb --version
```

</TabItem>
</Tabs>

4. **先停寫與 Drain**：透過部署平台停止新寫入與 Worker 認領，等待進行中的 HTTP、Job 與事件處理完成，記錄停寫時間點；若需同步備份 NATS JetStream，請依 Broker 備份政策另外執行（`pg_dump` 不含 Broker 資料）。

---

## 1. 建立自訂格式備份並驗證雜湊

在同一個終端機工作階段設定好 `SOURCE_DSN` 後執行（輸出檔案位於目前工作目錄）：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 驗證 SOURCE_DSN 為合法的 PostgreSQL URI
if ([string]::IsNullOrWhiteSpace($env:SOURCE_DSN)) { throw 'SOURCE_DSN is required' }
$sourceUri = $null
if (-not [System.Uri]::TryCreate($env:SOURCE_DSN, [System.UriKind]::Absolute, [ref]$sourceUri) -or
    $sourceUri.Scheme -notin @('postgres', 'postgresql')) {
    throw 'SOURCE_DSN must be an absolute postgres:// or postgresql:// URI'
}

# 1. 使用 custom format 建立備份檔
$backupFile = Join-Path $PWD ("pangolin-" + (Get-Date -Format 'yyyyMMdd-HHmmss') + '.dump')
pg_dump --format=custom "--file=$backupFile" "--dbname=$env:SOURCE_DSN"
if ($LASTEXITCODE -ne 0) { throw 'pg_dump failed' }

# 2. 驗證歸檔 TOC 清單可正常讀取（確認檔案未損毀）
pg_restore --list "$backupFile" > "$backupFile.list"
if ($LASTEXITCODE -ne 0) { throw 'pg_restore could not read the archive' }

# 3. 產生 SHA-256 供異機保存核對
Get-FileHash -Algorithm SHA256 -LiteralPath $backupFile
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
set -euo pipefail

# 驗證 SOURCE_DSN 為合法的 PostgreSQL URI
: "${SOURCE_DSN:?SOURCE_DSN is required}"
case "$SOURCE_DSN" in
  postgres://* | postgresql://*) ;;
  *) echo "SOURCE_DSN must be an absolute postgres:// or postgresql:// URI" >&2; exit 1 ;;
esac

# 1. 使用 custom format 建立備份檔
BACKUP_FILE="$PWD/pangolin-$(date +%Y%m%d-%H%M%S).dump"
pg_dump --format=custom --file="$BACKUP_FILE" --dbname="$SOURCE_DSN"

# 2. 驗證歸檔 TOC 清單可正常讀取（確認檔案未損毀）
pg_restore --list "$BACKUP_FILE" > "$BACKUP_FILE.list"

# 3. 產生 SHA-256 供異機保存核對
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$BACKUP_FILE"
else
  shasum -a 256 "$BACKUP_FILE"
fi
```

</TabItem>
</Tabs>

> **注意**：`pg_restore --list` 只證明檔案格式可讀；真正的可還原性必須透過下方第 2 步實際還原到空庫驗證。還原前請先依角色管理流程準備好原有的 owner、app、worker、migrator 角色與 grants。

---

## 2. 還原到全新的隔離空資料庫（不覆寫既有庫）

以下腳本會自動產生全新的還原資料庫名稱（`pangolin_restore_<32hex>`），絕不會覆寫或清除既有資料庫：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 驗證 ADMIN_DSN 與前一步產生的備份檔存在
if ([string]::IsNullOrWhiteSpace($env:ADMIN_DSN)) { throw 'ADMIN_DSN is required' }
$adminUri = $null
if (-not [System.Uri]::TryCreate($env:ADMIN_DSN, [System.UriKind]::Absolute, [ref]$adminUri) -or
    $adminUri.Scheme -notin @('postgres', 'postgresql')) {
    throw 'ADMIN_DSN must be an absolute postgres:// or postgresql:// URI'
}
if (-not (Test-Path -LiteralPath $backupFile -PathType Leaf)) { throw 'Backup file was not found in this PowerShell session' }

# 自動建立唯一的安全目標資料庫名稱與 URI
$restoreDb = "pangolin_restore_$([System.Guid]::NewGuid().ToString('N'))"
if ($restoreDb -notmatch '^pangolin_restore_[0-9a-f]{32}$') { throw 'Generated restore database name is invalid' }
$restoreUriBuilder = [System.UriBuilder]::new($adminUri)
$restoreUriBuilder.Path = "/$restoreDb"
$restoreDsn = $restoreUriBuilder.Uri.AbsoluteUri

# 建立全新空庫並使用 --exit-on-error 嚴格還原（遇錯即停）
createdb "--maintenance-db=$env:ADMIN_DSN" "$restoreDb"
if ($LASTEXITCODE -ne 0) { throw 'Could not create the new restore database' }

pg_restore --exit-on-error "--dbname=$restoreDsn" "$backupFile"
if ($LASTEXITCODE -ne 0) { throw 'pg_restore failed; keep the target isolated for investigation' }
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
set -euo pipefail

# 驗證 ADMIN_DSN 與前一步產生的備份檔存在
: "${ADMIN_DSN:?ADMIN_DSN is required}"
case "$ADMIN_DSN" in
  postgres://* | postgresql://*) ;;
  *) echo "ADMIN_DSN must be an absolute postgres:// or postgresql:// URI" >&2; exit 1 ;;
esac
test -f "${BACKUP_FILE:?BACKUP_FILE was not found in this shell session}"

# 自動建立唯一的安全目標資料庫名稱，並將 ADMIN_DSN 路徑替換為新資料庫名稱（保留 query 參數）
RESTORE_HEX=$( (uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid) | tr -d '-' | tr '[:upper:]' '[:lower:]')
RESTORE_DB="pangolin_restore_${RESTORE_HEX}"
case "$RESTORE_DB" in
  pangolin_restore_[0-9a-f]*) test "${#RESTORE_DB}" -eq 49 ;;
  *) echo "Generated restore database name is invalid" >&2; exit 1 ;;
esac
ADMIN_NO_QUERY="${ADMIN_DSN%%\?*}"
ADMIN_QUERY=""
if [ "$ADMIN_NO_QUERY" != "$ADMIN_DSN" ]; then
  ADMIN_QUERY="?${ADMIN_DSN#*\?}"
fi
RESTORE_DSN="${ADMIN_NO_QUERY%/*}/${RESTORE_DB}${ADMIN_QUERY}"

# 建立全新空庫並使用 --exit-on-error 嚴格還原（遇錯即停）
createdb --maintenance-db="$ADMIN_DSN" "$RESTORE_DB"
pg_restore --exit-on-error --dbname="$RESTORE_DSN" "$BACKUP_FILE"
```

</TabItem>
</Tabs>

若 `pg_restore` 失敗，請保留該隔離資料庫與命令紀錄供排查，下次重試時另建新名稱，不要在同一目標上重複覆寫。

---

## 3. 獨立連線核對 4 大關鍵面向（尚未對外開流量前）

在確認這 4 項全部通過前，**先不要把 API 或 Worker 指到還原資料庫**：

1. **資料與 Owner 完整性**：逐表比對列數與穩定 checksum；確認每筆業務資料的 `tenant_id`、`owner_scope_kind`、`owner_scope_id`、中立 `resource_scopes` catalog、Organization membership，以及待處理 Outbox／Inbox／Job 皆完整保留（絕不把未知或已撤銷 owner 改成 tenant）。
2. **RLS 與最小權限角色**：確認租戶業務表仍啟用 `RLS` 與 `FORCE ROW LEVEL SECURITY`、Policy 僅允許精確 owner、且 `app`／`worker` 帳號不是 `superuser`／`table owner`／`BYPASSRLS`。實際以 `app` 帳號測試：自己 owner 可見、跨 Tenant／Scope 一律零筆或拒絕。
3. **Schema 與 Profile 一致性**：核對 Goose Migration 版本完整，並以目標 Runtime 相容性檢查確認 Profile 一致（不可將含組織資料的 Schema 接給 `tenant-only` 程序）。
4. **待處理工作與 Broker 對帳**：核對 Outbox、Inbox、Jobs 各狀態筆數、最舊時間與租約，並確認 Broker 備份與去重視窗無虞後再恢復 Worker。

> **RPO／RTO 實測原則**：請把核對紀錄、備份 hash、來源 commit 與 migration 版本存入維運紀錄。**停寫演練不能宣稱為生產 RPO**；真實 RPO 須依生產備份週期與持續寫入量測，RTO 從事故開始計時至服務恢復且對帳完成。

---

## 4. 同源升級檢查流程

0.1.0 預覽版應確保應用 Runtime、`pangolin` CLI 與生成 Template 來自**同一 Framework commit**（詳細升級步驟見[選擇版本與升級](../reference/upgrading.md)與[規格與升級 CLI 手冊](../reference/cli-openapi-upgrade.md)）。在 CLI 生成專案根目錄依序執行：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
go test ./...
if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
& $pangolin doctor
if ($LASTEXITCODE -ne 0) { throw 'pangolin doctor failed' }
go run ./cmd/migrate
if ($LASTEXITCODE -ne 0) { throw 'migration command failed' }
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
set -euo pipefail
go test ./...
"$PANGOLIN" doctor
go run ./cmd/migrate
```

</TabItem>
</Tabs>

- `go run ./cmd/migrate` 適用於內建該入口的生成專案（需設定 `PANGOLIN_MIGRATION_DSN`）；自建專案則呼叫你自己的部署遷移入口（API 啟動永不自動執行 DDL）。
- 切換 `tenant-only` 與 `organization-enabled` 必須規劃並執行[前向遷移](../data/migrations.md)，絕不能只改設定檔；事件 `SchemaVersion` 亦獨立於 Framework 版本，升級框架不會改寫已發布事件格式。

---

## 5. 恢復服務與異常退場原則

確認所有核對通過後，將 API 與 Worker 指向還原目標、啟動相符 Profile 的 Runtime，待 `/readyz` 回應 `204` 且觀測面板確認佇列健康後，再恢復對外新寫入。

若升級遷移不相容或核對失敗，請保持還原目標隔離、保留原庫供比對，採用**經審查的前向修復**或從已驗證備份重建新目標——**絕不把 Down Migration 或覆寫正式資料庫當成常規回復手段**。

## 下一步

- [部署資料庫遷移](../data/migrations.md)
- [選擇版本與升級](../reference/upgrading.md)
- [容量、停機與恢復](operations.md)
