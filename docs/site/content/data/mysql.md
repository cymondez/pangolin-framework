---
title: MySQL Adapter 組裝
description: 在組裝端接上 MySQL，應用與 Repository 保持使用中立契約。
---

MySQL Adapter 使用 go-sql-driver/mysql。先依[中立資料存取](database.md)建立 Repository／NewDatabaseService；Driver 型別及 DSN 僅留在組裝端。

## 完整連線組裝

以下完整檔案可放在自己的 composition/mysql.go。dsn 由可信啟動設定或密鑰服務傳入，不在 Repository 或公開請求中選擇連線。

```go
package composition

import (
    "context"
    "errors"
    "github.com/cymondez/pangolin-framework/persistence/database"
    "github.com/cymondez/pangolin-framework/persistence/mysql"
)

func OpenMySQLResource(ctx context.Context, dsn string) (database.Resource, func() error, error) {
    resource, err := mysql.Open(mysql.Config{DSN: dsn, MaxOpen: 4})
    if err != nil { return nil, nil, err }
    if err = resource.Pool().PingContext(ctx); err != nil {
        return nil, nil, errors.Join(err, resource.Pool().Close())
    }
    return resource, resource.Pool().Close, nil
}
```

回傳的 Resource 交給 NewDatabaseService；連線池建立者在 Drain／UOW 清理完成後呼叫 cleanup。已有 sql.DB 的組裝可用 mysql.Wrap；其生命週期仍由原建立者管理。

## 能力與資料保護

MySQL 支援 ReadCommitted、RepeatableRead、Serializable 及引擎唯讀交易，SQL Placeholder 使用問號。Repository 從 callback context 取得 database.Current，不直接取得 Tx 或 Pool。

MySQL 沒有 PostgreSQL RLS。完整可信 Tenant／owner 條件、複合唯一鍵與外鍵仍需由 Repository／應用 schema 落實；若組裝要求引擎 RLS，database.Admit 必須拒絕。引擎專屬 migration、治理、Audit、事件或 Jobs 不能只改 DSN 就當作已移植，見[相容性](../reference/compatibility.md)。

錯誤保留 Driver cause；Commit 無法確認時保留 Unknown，以原操作識別對帳，不自動重跑 Use Case。Row 必須 Scan，Rows 必須 Close 並檢查 Err，不能在同一 Executor 平行執行 SQL。
