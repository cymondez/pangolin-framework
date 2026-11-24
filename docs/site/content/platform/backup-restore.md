---
title: 備份、還原與升級檢查
description: 以 PostgreSQL 工具備份資料，在新資料庫驗證 owner、RLS 與待處理工作，再安全部署相容版本。
---

本指南示範如何用 PostgreSQL 工具建立備份、在新的資料庫還原，並核對資料 owner、RLS 與待處理工作後恢復服務。以下命令使用 PowerShell 7；請依部署程序選擇核准的維運主機與資料目錄。

## 開始前

1. 在自己的應用部署 checkout 根目錄確認目前產品版本、來源 commit、資料庫 Profile、migration 版本及使用中的事件 schema。預覽版請固定到明確的 Framework commit。
2. 備份時以核准的 secret 注入方式設定 SOURCE_DSN；還原時另準備管理連線 ADMIN_DSN。兩者都必須是 `postgres://` 或 `postgresql://` PostgreSQL URI。不要把含密碼的 URI 寫在命令列、文件或 shell history。環境變數可避免把 URI 文字留在 history，但 URI 仍可能出現在程序參數；正式部署可使用不含密碼的 URI，並由受保護的 PGPASSFILE 或部署連線檔提供憑證。
3. 確認安裝的 `pg_dump`、`pg_restore` 與 `createdb` client 支援目前 PostgreSQL server 版本。應用、Worker 和部署 migrator 使用不同的最小權限角色；不要把教學 fixture 帳號或密碼當正式設定。
4. 使用你的服務管理平台停止新寫入及 Worker 認領，等待活躍 HTTP、Job 與事件處理完成。記下停寫時間與待處理工作狀態；需要同步恢復 Broker 時，另依 Broker 自己的備份政策操作。

以下命令範例使用 PowerShell 7。先在核准的備份工作目錄開啟同一個 PowerShell 工作階段；備份檔路徑以目前目錄 `$PWD` 為準。先檢查 PostgreSQL 工具版本，每個原生命令後都確認其 exit code：

~~~powershell
pg_dump --version
if ($LASTEXITCODE -ne 0) { throw 'pg_dump --version failed' }
pg_restore --version
if ($LASTEXITCODE -ne 0) { throw 'pg_restore --version failed' }
createdb --version
if ($LASTEXITCODE -ne 0) { throw 'createdb --version failed' }
~~~

## 1. 建立並檢查備份

在上述工作階段設定好 SOURCE_DSN 後，將備份寫入受保護的目前目錄：

~~~powershell
if ([string]::IsNullOrWhiteSpace($env:SOURCE_DSN)) { throw 'SOURCE_DSN is required' }
$sourceUri = $null
if (-not [System.Uri]::TryCreate($env:SOURCE_DSN, [System.UriKind]::Absolute, [ref]$sourceUri) -or
    $sourceUri.Scheme -notin @('postgres', 'postgresql')) {
    throw 'SOURCE_DSN must be an absolute postgres:// or postgresql:// URI'
}

$backupFile = Join-Path $PWD ("pangolin-" + (Get-Date -Format 'yyyyMMdd-HHmmss') + '.dump')
pg_dump --format=custom "--file=$backupFile" "--dbname=$env:SOURCE_DSN"
if ($LASTEXITCODE -ne 0) { throw 'pg_dump failed' }

pg_restore --list "$backupFile" > "$backupFile.list"
if ($LASTEXITCODE -ne 0) { throw 'pg_restore could not read the archive' }

Get-FileHash -Algorithm SHA256 -LiteralPath $backupFile
~~~

預期得到 custom-format dump、可讀取的 TOC 清單及 SHA-256。將備份與 checksum 傳到核准的異機保存位置，並確認副本 hash 相同。pg_restore --list 只驗證 archive 可讀；完整可還原性要用新資料庫實際演練。

pg_dump 備份單一資料庫，不會建立 PostgreSQL 全域角色。還原前，依你的角色管理程序準備原有 owner、app、worker、migrator 角色及必要 grants。NATS JetStream 等 Broker 資料也不是這份資料庫 dump 的一部分，須按 Broker 的備份政策另外處理。

## 2. 還原到新的空資料庫

還原目標名稱由 PowerShell 以 GUID 產生，僅含安全的 ASCII 小寫字母、數字與底線，並以同一個 ADMIN_DSN 的 UriBuilder 衍生目標 URI。`createdb` 連到 ADMIN_DSN 所指的同一個 PostgreSQL 管理端點建立該資料庫；`pg_restore` 使用程式衍生的 URI 指向新資料庫。你不需要另外填 RESTORE_DB 或 RESTORE_DSN。`createdb` 遇到已存在的名稱會失敗，程式不會清除或覆寫任何既有資料庫。保留來源資料庫不變，也不要把服務連到未完成核對的還原目標。

~~~powershell
if ([string]::IsNullOrWhiteSpace($env:ADMIN_DSN)) { throw 'ADMIN_DSN is required' }
$adminUri = $null
if (-not [System.Uri]::TryCreate($env:ADMIN_DSN, [System.UriKind]::Absolute, [ref]$adminUri) -or
    $adminUri.Scheme -notin @('postgres', 'postgresql')) {
    throw 'ADMIN_DSN must be an absolute postgres:// or postgresql:// URI'
}
if (-not (Test-Path -LiteralPath $backupFile -PathType Leaf)) { throw 'Backup file was not found in this PowerShell session' }

$restoreDb = "pangolin_restore_$([System.Guid]::NewGuid().ToString('N'))"
if ($restoreDb -notmatch '^pangolin_restore_[0-9a-f]{32}$') { throw 'Generated restore database name is invalid' }
$restoreUriBuilder = [System.UriBuilder]::new($adminUri)
$restoreUriBuilder.Path = "/$restoreDb"
$restoreDsn = $restoreUriBuilder.Uri.AbsoluteUri

createdb "--maintenance-db=$env:ADMIN_DSN" "$restoreDb"
if ($LASTEXITCODE -ne 0) { throw 'Could not create the new restore database' }

pg_restore --exit-on-error "--dbname=$restoreDsn" "$backupFile"
if ($LASTEXITCODE -ne 0) { throw 'pg_restore failed; keep the target isolated for investigation' }
~~~

預期 createdb 與 pg_restore 都以 exit code 0 結束。--exit-on-error 讓還原遇到錯誤時停止。若失敗，保留目標資料庫及命令紀錄供調查，不要在同一個目標上反覆覆寫；依變更程序準備另一個新名稱後重試。

## 3. 從獨立連線核對還原結果

還原完成後，先不要啟動 API 或 Worker。使用新的唯讀管理連線比較來源紀錄和還原目標，再以正式 app／worker 角色進行隔離探測。

- **資料與 owner**：逐表比較預期列數及穩定 checksum；確認每筆資料的 TenantID、ScopeKind、ScopeID、主鍵與 owner 關聯都保留。檢查中立 scope catalog、Organization membership，以及尚未處理的 Outbox／Inbox／Job 都仍有原 owner。不要把未知或已撤銷的 owner 改成 tenant owner。
- **RLS 與角色**：確認租戶業務表仍啟用 RLS 與 FORCE ROW LEVEL SECURITY，實際 policy 集合只允許精確 owner 條件；app／worker 角色不是 superuser、table owner 或 BYPASSRLS。使用 app／worker 連線測試自己的 owner 可見，跨 Tenant／Scope 的讀寫皆受拒絕。
- **Schema 與 Profile**：確認 migration 版本完整，並以目標 Runtime 的相容性檢查確認 Profile／schema 相符。Scope-aware schema 必須搭配相符的應用組裝；tenant-only 程序不應連到含有 Organization 資料的 schema。
- **待處理工作**：比較 Outbox、Inbox、Jobs 各狀態的筆數、owner、最舊時間與租約資訊。先確認 Broker 備份狀態與 Consumer 的重送／去重條件，再恢復 Worker；配送是 at-least-once，重複外部副作用仍須使用穩定冪等鍵。

把核對結果、備份 hash、來源 commit、migration 版本及目標連線識別保存到維運紀錄。RPO 依正式環境的備份頻率、持續寫入與還原後差異量測；停寫種子資料的演練或 pg_dump 啟動前的時間間隔不能當作生產 RPO，也不能代表精確 snapshot 時點。RTO 應從事故或切換開始計時，直到服務恢復且資料與待處理工作完成對帳。

## 4. 按同一來源升級

0.1.0 目前是預覽版，尚無正式產品 tag；使用預覽來源時記錄明確 Framework commit。應用 Runtime、pangolin CLI 與生成 Template 使用同一來源版本。生成專案升級時，整套更新 framework/ 與 runtime-manifest.json，或由同一 checkout 重新生成；不要混用不同來源的 Runtime、CLI、Template 或 checksum。

在生成專案根目錄先檢查 Runtime 與應用，再由部署 migrator 套用 migration：

~~~powershell
go test ./...
if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
pangolin doctor
if ($LASTEXITCODE -ne 0) { throw 'pangolin doctor failed' }
go run ./cmd/migrate
if ($LASTEXITCODE -ne 0) { throw 'migration command failed' }
~~~

go run ./cmd/migrate 只適用帶有該入口的生成專案；其他應用使用自己經審查的部署 migration entrypoint。API 啟動時做 schema 相容性檢查，不會自動執行遷移。

tenant-only 與 organization-enabled 是不同依賴圖及 schema。啟用 Organization 要先規劃並執行[前向遷移](../data/migrations.md)，不能只改設定檔切換 Profile。已含非 Tenant owner 或待處理工作時，移除 Organization Provider 前必須完成完整 inventory 與恢復計畫。

事件 SchemaVersion 與 Framework 產品版本分開管理。升級 Framework 不會自動升級或重寫已發布的 event payload；需要變更格式時，維護獨立版本化契約及相容讀取／Upcaster。

## 5. 恢復服務

確認資料、owner、RLS、schema 相容性、Broker 狀態及待處理工作都核對通過後，使用你的部署平台將 API 與 Worker 指向還原目標，啟動與資料庫一致的 Runtime／Profile。先等待 Ready，檢查健康狀態與佇列，再恢復新寫入。

若前向 migration 不相容或核對失敗，保持還原目標隔離，保留原資料庫供比對，採用經審查的前向修復或從已驗證備份建立另一個新目標。不要以 Down migration 或覆寫正式資料庫作為一般回復步驟。

下一步：[資料庫遷移](../data/migrations.md)、[選擇版本與升級](../reference/upgrading.md)、[容量、停機與恢復](operations.md)。
