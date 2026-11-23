---
title: 部署資料庫遷移
description: 用 Goose 與全域部署鎖套用 schema，讓 API 啟動只做相容性檢查。
---

本頁適用已有 PostgreSQL 應用的部署者。Goose SQL migration 由部署程序執行，API 啟動只檢查相容性；不要將部署用 DDL 權限交給業務連線。

## 註冊應用需要的 schema

各 package 提供 embedded migration。多數檔案位於 `migrations` 子目錄，先使用 `fs.Sub` 取得；`organization.Migrations()` 已回傳可直接使用的檔案系統。

| 功能 | Migration 來源 |
|---|---|
| 本地 Tenant 資格 | tenancy/postgres.Migrations |
| 必要 Audit | platform/audit.Migrations |
| 中立 owner catalog | migration.ScopeMigrations |
| 可選組織治理 | organization.Migrations() |
| 可靠事件 | events/postgres.Migrations |
| Command 冪等 | idempotency.Migrations |

tenant-only 不載入 Organization migration。使用 `migration.JoinFS` 將來源掛到各自目錄，再以 `ModuleMigrations{Module, Directory, DependsOn}` 宣告相依。版本號在整個應用唯一，依賴模組的版本須早於使用者；缺依賴、循環、重複或倒序在 DDL 前失敗。

以下是你應用 `setup/deployment.go` 的完整檔案。source／modules 由部署入口依上表組裝，minimum／maximum 是這份應用能接受的 schema 版本範圍，不能把其他應用的版本上限直接照抄。

```go
package setup

import (
    "context"
    "database/sql"
    "io/fs"
    "github.com/cymondez/pangolin-framework/persistence/migration"
    _ "github.com/jackc/pgx/v5/stdlib"
)

func Deploy(ctx context.Context, db *sql.DB, source fs.FS,
    modules []migration.ModuleMigrations, minimum, maximum int64) error {
    runner, err := migration.NewPostgres(db, source, minimum, maximum)
    if err != nil { return err }
    if err = runner.Apply(ctx, modules); err != nil { return err }
    return runner.CheckCompatibility(ctx)
}
```

放入自己的 Go module 後執行 `go build ./...`。呼叫前由部署程序開啟 DB；SQL Pool 至少允許兩條連線，結束由建立者 Close。Apply 包住整個 Goose 操作取得全域 session advisory lock，包括空庫版本表初始化；同時部署會序列化。取消仍以獨立有界 context 清理，無法確認解鎖的連線會丟棄。

## API 啟動前檢查

以 API 的唯讀 schema 檢查相依建立 Runner，把 `CheckCompatibility(ctx)` 接到 `runtime.Options.StartupChecks`。檢查在 Provider seal 後、任何 Module.Start 前執行；失敗就不 Ready。Options 請使用具名欄位，以便新增設定時仍易維護。

更換 Scope Provider 或移除模組時，同時使用 `postgres.ScopeInventory` 的可信完整 metadata 連線及表描述，呼叫 `registry.CheckInventory`。需要納入中立 catalog、業務 owner、未完成事件與 Job；一般 app Pool 的 RLS 過濾結果不足以證明可以降級。看不到完整資料或缺 Kind 就拒絕啟動。

## 從 TenantOnly 升級

先停止接入、停止認領並 Drain，備份及對帳。業務表明示回填 `(TenantID, tenant, TenantID)`，增加完整 owner 唯一鍵／複合外鍵，參照中立 resource_scopes；同一交易移除舊 permissive policies 再安裝精確 owner RLS，避免 policies 被 OR 放寬。

Bookstore 提供可選 `infrastructure.ScopeUpgrade` 的部署檔，會把 books 改為 scope_books，配合現有 ScopedRepository。舊 tenant-only API 相容性檢查會拒絕這份 schema；它不會自動變成 Organization API。請先確認要使用的 Repository 與新程序，再執行升級。

已套用 migration 不改寫，優先新增前向修復；不要以 allow-missing 掩蓋版本順序。Provider 替換與 ownerless hook 的完整處理見[組織治理](../security/organizations.md)。
