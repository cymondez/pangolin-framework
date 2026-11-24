---
title: 中立資料存取與資料庫選擇
description: 以相同的 UOW 與 Repository Port 使用 PostgreSQL、MySQL 或 SQLite。
---

應用服務依賴自己的 Repository 介面。Repository 實作使用 `persistence/database`，連線與引擎選擇留在組裝端。換資料庫時，不必把 Application 的介面改成 PostgreSQL、MySQL 或 SQLite 型別。

先閱讀[工作單元](unit-of-work.md)，了解 Within callback 與 Commit 邊界。以下完整檔案可放在自己的 `books/repository.go`，與該頁的 `books/service.go` 一起使用。

## 選擇 Adapter

| 引擎 | 組裝端套件 | 連線設定 | 查詢參數 |
|---|---|---|---|
| [PostgreSQL](postgresql.md) | persistence/postgres | pgx Pool | $1、$2 |
| [MySQL](mysql.md) | persistence/mysql | MySQL DSN／Config | ? |
| [SQLite](sqlite.md) | persistence/sqlite | 檔案路徑或記憶體設定 | ? |

三種 Adapter 都交給同一個 uow.NewManager，Repository 都從 callback context 呼叫 database.Current。Driver 型別留在組裝／基礎設施；Domain、Application 不直接開啟連線或取得 Tx。

## 完整 Repository 檔案

範例的 books 表由你的應用 migration 建立，欄位為 tenant_id、owner_scope_kind、owner_scope_id、id、title；唯一鍵包含完整 owner 與 id。這份 Repository 不負責建立資料表，也不把外部 DTO 的 owner 寫進資料庫。

```go
package books

import (
    "context"
    "crypto/rand"
    "fmt"
    "time"

    "github.com/cymondez/pangolin-framework/persistence/database"
    "github.com/cymondez/pangolin-framework/persistence/uow"
)

type Repository struct { dialect database.Dialect }

func (r Repository) Insert(ctx context.Context, title string) error {
    executor, err := database.Current(ctx)
    if err != nil { return err }
    owner, err := database.CurrentOwner(ctx)
    if err != nil { return err }
    query := fmt.Sprintf(
        "INSERT INTO books (tenant_id, owner_scope_kind, owner_scope_id, id, title) VALUES (%s,%s,%s,%s,%s)",
        r.dialect.Placeholder(1), r.dialect.Placeholder(2),
        r.dialect.Placeholder(3), r.dialect.Placeholder(4), r.dialect.Placeholder(5))
    _, err = executor.Exec(ctx, query, owner.TenantID, owner.ScopeKind, owner.ScopeID, rand.Text(), title)
    return err
}

func (r Repository) GetTitle(ctx context.Context, id string) (string, error) {
    executor, err := database.Current(ctx)
    if err != nil { return "", err }
    owner, err := database.CurrentOwner(ctx)
    if err != nil { return "", err }
    query := fmt.Sprintf(
        "SELECT title FROM books WHERE tenant_id=%s AND owner_scope_kind=%s AND owner_scope_id=%s AND id=%s",
        r.dialect.Placeholder(1), r.dialect.Placeholder(2),
        r.dialect.Placeholder(3), r.dialect.Placeholder(4))
    var title string
    err = executor.QueryRow(ctx, query, owner.TenantID, owner.ScopeKind, owner.ScopeID, id).Scan(&title)
    return title, err
}

func NewDatabaseService(resource database.Resource) (*Service, error) {
    if resource == nil { return nil, fmt.Errorf("database resource is required") }
    if err := database.Admit(resource.Capabilities(), database.Requirements{
        Isolation: uow.Serializable,
    }); err != nil { return nil, err }
    manager, err := uow.NewManager(resource, uow.DefaultOptions{
        DefaultTimeout: 5*time.Second, CleanupTimeout: 2*time.Second,
        HookTimeout: time.Second, MaxRequiresNewDepth: 4,
        DefaultIsolation: uow.Serializable,
    })
    if err != nil { return nil, err }
    return NewService(manager, Repository{dialect: resource.Dialect()}), nil
}
```

由組裝端建立選定的 Resource，檢查連線、部署對應 migration，再傳給 NewDatabaseService。這裡明示選擇三個引擎都能提供的 Serializable；未指定 Manager 隔離政策時，既有預設仍是 ReadCommitted，SQLite 組裝需明示 Serializable。

呼叫 Service.Create 時傳入入口建立的可信 Tenant／Scope context。直接使用 Repository.GetTitle 時，同樣先由應用服務開啟唯讀 UOW，再傳入 callback context。沒有 UOW、無有效 binding、唯讀寫入、錯誤 Resource 或已結束 context 都會被拒絕；不要在 Repository 內自行 Begin，或退回 Pool 自動提交。

## 保留引擎差異

| 能力 | PostgreSQL | MySQL | SQLite |
|---|---|---|---|
| 引擎 RLS | 支援，表格仍須正確設定 FORCE RLS | 不提供 | 不提供 |
| 引擎唯讀交易 | 支援 | 支援 | 不提供操作級切換；UOW 限制宣告的寫入 |
| RequiresNew | 獨立連線／交易 | 獨立連線／交易 | 獨立連線；父層持有寫鎖時，子寫入可失敗 |

如果應用要求引擎 RLS 或唯讀交易，啟動時以 database.Admit 檢查；缺能力就拒絕，不偷偷放寬。完整 owner 條件、複合唯一鍵與外鍵仍適用。SQLite 也必須啟用外鍵檢查。

三引擎支援的是中立資料存取與 UOW。既有完整 Bookstore、治理、可靠事件、Jobs 等 PostgreSQL 專用 Adapter 仍使用各自的 SQL／migration；不能只改 DSN 就當成已移植。需要原生 pgx／RLS 組裝細節時，繼續閱讀 [PostgreSQL Adapter](postgresql.md)。

## 錯誤與清理

使用 errors.Is 判斷 database.ErrNoRows；其他錯誤保留 Driver cause。Commit Unknown 應以原操作識別對帳，不自動重跑 Use Case。查詢多列時關閉 Rows 並檢查 Err；不把 cursor 帶離 callback。

連線池由建立它的組裝端擁有，在停止接入、Drain 與 UOW 清理後關閉。下一步閱讀[冪等操作](idempotency.md)及[遷移](migrations.md)，並依選定的持久化 Adapter 確認支援範圍。
