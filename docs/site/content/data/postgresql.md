---
title: PostgreSQL Adapter 組裝
description: 在組裝端選擇 PostgreSQL，應用與 Repository 使用中立資料存取契約。
---

先依[中立資料存取](database.md)建立 Repository 與 NewDatabaseService。本頁只處理 PostgreSQL 連線、RLS 與引擎原生功能；應用服務的建構函式及一般資料操作不使用資料庫名稱或 pgx 型別。

## 在組裝端建立 Resource

以下完整檔案可放在應用的 composition/database.go。連線池的建立者負責在停止接入、Drain 與 UOW 清理後呼叫回傳的 cleanup。

```go
package composition

import (
    "context"
    "github.com/cymondez/pangolin-framework/persistence/database"
    "github.com/cymondez/pangolin-framework/persistence/postgres"
    "github.com/jackc/pgx/v5/pgxpool"
)

func OpenPostgresResource(ctx context.Context, dsn string) (database.Resource, func(), error) {
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil { return nil, nil, err }
    if err = pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, nil, err
    }
    return &postgres.Resource{Pool: pool}, pool.Close, nil
}
```

將回傳的 database.Resource 交給前一頁的 NewDatabaseService。Repository 使用 database.Current(ctx)、可信 owner 與 Resource 的 Dialect，換引擎時保留相同 Application／Repository Port；引擎選擇與連線設定留在此組裝層。

## 租戶與 owner 隔離

每筆交易設定 transaction-local Tenant／Scope。Repository 的完整 owner 等值條件與 Row-Level Security（RLS）共同隔離資料。應用帳號不能是 superuser、資料表擁有者或有 BYPASSRLS；受保護資料表需正確設定 FORCE RLS。

中立 Resource 的 Capabilities 宣告引擎能力，不能證明應用資料表已安裝正確 policy。先完成 migration，再以實際應用帳號驗證隔離；見[多租戶](../security/multi-tenancy.md)與[資源 Scope](../security/resource-scopes.md)。

## 原生擴充與共用生命週期

需要 pgx 特有功能的 Infrastructure Adapter 可以明示使用原生 postgres.Current；這不是 Application 或一般 Repository 的依賴。原生與中立 Executor 使用同一個 UOW session；同一工作單元內依序執行 SQL，不能藉兩個入口同時操作交易。

QueryRow 必須 Scan，Rows 必須 Close 並檢查 Err；不將 cursor 帶離 callback。沒有有效 UOW、唯讀寫入、錯誤 resource key、已結束視圖或重疊 SQL 都拒絕；Repository 不自行 Begin／Commit，也不退回 Pool 自動提交。

完整 Bookstore、治理、Audit、可靠事件及 Jobs 的既有持久化 Adapter 仍使用各自的引擎專用 SQL／migration。採用中立 Port 不會自動移植這些功能；選擇 MySQL／SQLite 時先確認[相容性](../reference/compatibility.md)與組裝要求，不能放寬原 Profile 的隔離保證。
