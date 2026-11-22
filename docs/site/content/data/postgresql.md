---
title: PostgreSQL 資料存取
description: 組裝 pgx Pool 與 UOW，讓 Repository 使用目前操作的交易。
---

應用服務透過 Repository 介面讀寫資料，PostgreSQL 實作則放在你的資料存取層。先完成 [Bookstore](../tutorials/bookstore.md)，即可在可執行應用中查看這些元件如何組合。

## 組裝連線與 Manager

組裝端使用 `pgxpool.New` 建立連線池，再將 `&postgres.Resource{Pool: pool}` 交給 `uow.NewManager`。設定 DefaultTimeout、CleanupTimeout、HookTimeout 與 MaxRequiresNewDepth，並檢查建構錯誤。

Bookstore 的連線設定為 `PANGOLIN_DSN`；API 與 Worker 使用不同的資料庫角色，請依教學分別設定。

## 在 Repository 取得交易

以下方法放在你自己的 Repository 實作中。ctx 必須是 Within 傳入的工作 context：

```go
package books

import (
    "context"
    "crypto/rand"
    "fmt"
    "time"
    "github.com/cymondez/pangolin-framework/persistence/postgres"
    "github.com/cymondez/pangolin-framework/persistence/uow"
    "github.com/cymondez/pangolin-framework/security/scope"
    "github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{}

func (Repository) Insert(ctx context.Context, title string) error {
    executor, err := postgres.Current(ctx, true)
    if err != nil { return err }
    binding, ok := scope.Current(ctx)
    if !ok || binding.Ref.Kind != "tenant" { return fmt.Errorf("tenant scope is required") }
    _, err = executor.Exec(ctx,
        "INSERT INTO pangolin.books (tenant_id, id, title) VALUES ($1, $2, $3)",
        binding.Ref.TenantID, rand.Text(), title)
    return err
}

func (Repository) GetTitle(ctx context.Context, id string) (string, error) {
    executor, err := postgres.Current(ctx, false)
    if err != nil { return "", err }
    binding, ok := scope.Current(ctx)
    if !ok || binding.Ref.Kind != "tenant" { return "", fmt.Errorf("tenant scope is required") }
    var title string
    err = executor.QueryRow(ctx,
        "SELECT title FROM pangolin.books WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL",
        binding.Ref.TenantID, id).Scan(&title)
    return title, err
}

func (Repository) Titles(ctx context.Context) ([]string, error) {
    executor, err := postgres.Current(ctx, false)
    if err != nil { return nil, err }
    binding, ok := scope.Current(ctx)
    if !ok || binding.Ref.Kind != "tenant" { return nil, fmt.Errorf("tenant scope is required") }
    rows, err := executor.Query(ctx,
        "SELECT title FROM pangolin.books WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY id LIMIT 20",
        binding.Ref.TenantID)
    if err != nil { return nil, err }
    defer rows.Close()
    var titles []string
    for rows.Next() {
        var title string
        if err := rows.Scan(&title); err != nil { return nil, err }
        titles = append(titles, title)
    }
    return titles, rows.Err()
}

func NewPostgresService(pool *pgxpool.Pool) (*Service, error) {
    manager, err := uow.NewManager(&postgres.Resource{Pool: pool}, uow.DefaultOptions{
        DefaultTimeout: 5*time.Second, CleanupTimeout: 2*time.Second,
        HookTimeout: time.Second, MaxRequiresNewDepth: 4,
    })
    if err != nil { return nil, err }
    return NewService(manager, Repository{}), nil
}
```

將這份程式存為 `books/repository.go`，與上一頁的 `books/service.go` 放在同一個 package。它實作 Service 需要的 Insert；NewPostgresService 接收你建立的 pgx Pool，回傳已組裝的 Service。Pool 由建立它的應用在結束時關閉。

這份 Repository 使用 Bookstore 的資料表，從操作 context 取得可信 Tenant。每次查詢／寫入都使用 Within callback 的 ctx。Bookstore 自己的完整 Repository 位於 `examples/bookstore/infrastructure/repository.go`。

## 使用時要注意

- SQL 使用參數，不把請求值直接拼進字串。
- QueryRow 必須呼叫 Scan，讀取結果或錯誤並釋放占用。
- Query 的 Rows 必須關閉並檢查 Err；不能把 cursor 帶出 UOW 或直接交給長時間串流。
- 同一工作單元內依序執行 SQL，不同時啟動多個 goroutine 使用交易。
- 不要在 Repository 自行 Begin／Commit，或在沒有 UOW 時改用 Pool 寫入。

## 隔離租戶資料

每筆交易會設定 transaction-local tenant。Repository 的查詢條件與 PostgreSQL Row-Level Security（RLS，資料列安全政策）共同限制資料可見範圍。

應用連線帳號不能使用 superuser、資料表擁有者或 BYPASSRLS 權限。[多租戶指南](../security/multi-tenancy.md)說明可信 Tenant 如何從登入身份進入交易。

Exec、QueryRow 與 Query 都受操作生命週期限制。尚未關閉的 Rows 仍算占用，同一 Executor 的重疊 SQL 返回 UOW_BUSY，且根交易維持 rollback-only。資料庫延遲讀取錯誤即使被捕捉也不能讓根提交；Rows.Conn 不會提供底層連線。

寫入 RETURNING 使用 `postgres.Current(work, true)` 取得 Executor，讓唯讀子範圍的限制仍能生效。Repository 不自行取得第二個 Pool；同根 resource key 不相符會返回 UOW_RESOURCE_MISMATCH。

## 更新與刪除的版本競爭

Bookstore 的 VersionedRepository.Update／Delete 接收 expected version。SQL 同時比對 Tenant、ID、version 與 deleted_at；只有一個相同版本的併發修改能成功，其他回 CONFLICT。Delete 更新 deleted_at，Get 不回傳已刪除資料；目前 HTTP 教學仍只提供建立與查詢路由。

這個 Adapter 不提供 Batch 入口。完整支援表見[相容性](../reference/compatibility.md)；組織資料要選擇獨立的 [ScopeAware Repository](../security/resource-scopes.md)。
