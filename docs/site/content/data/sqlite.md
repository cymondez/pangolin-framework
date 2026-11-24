---
title: SQLite Adapter 組裝
description: 使用 SQLite 檔案或記憶體資料庫，保留中立 Repository 與 UOW。
---

SQLite 使用 pure Go 的 modernc.org/sqlite Driver，不需要額外資料庫服務或 CGO。先依[中立資料存取](database.md)建立 Repository／NewDatabaseService，再由組裝端選擇 SQLite；Application 不依賴 SQLite 型別。

## 完整連線組裝

將以下檔案放在自己的 composition/sqlite.go。資料檔路徑來自可信啟動設定，不能直接採用請求中的路徑。

```go
package composition

import (
    "context"
    "errors"
    "github.com/cymondez/pangolin-framework/persistence/database"
    "github.com/cymondez/pangolin-framework/persistence/sqlite"
)

func OpenSQLiteResource(ctx context.Context, path string) (database.Resource, func() error, error) {
    resource, err := sqlite.Open(sqlite.Config{Path: path, MaxOpen: 4})
    if err != nil { return nil, nil, err }
    if err = resource.Pool().PingContext(ctx); err != nil {
        return nil, nil, errors.Join(err, resource.Pool().Close())
    }
    return resource, resource.Pool().Close, nil
}
```

傳入例如 app.db 的應用資料檔路徑；回傳的 Resource 交給 NewDatabaseService，cleanup 由建立者在 Drain／UOW 清理完成後呼叫。改用 Memory:true 可明示選擇記憶體資料庫；它的存在時間受連線池生命週期影響，不能當作跨程序儲存。

## 交易與隔離

Manager 的 DefaultIsolation 明示設為 uow.Serializable；[共用組裝範例](database.md)已設定。明示 ReadCommitted／RepeatableRead 會被拒絕，不靜默轉換。每個 pooled connection 都啟用外鍵檢查；應用仍需部署自己的 schema、完整 owner 複合唯一鍵及外鍵。

Required 共用根交易，RequiresNew 使用另一連線／交易。先完成的子交易不隨父回滾；父層已持有寫鎖時，子寫入可能因 SQLite 單一 writer 限制失敗。應用檢查錯誤，框架不改用 Savepoint 或隱式重試。

SQLite 沒有引擎 RLS 或每操作切換的引擎唯讀交易；UOW 限制宣告的寫入，Repository 查詢保留完整可信 owner。要求缺少的引擎能力時用 database.Admit 拒絕組裝。既有完整 Bookstore／治理／事件等持久化 Adapter 仍有專屬 SQL，見[相容性](../reference/compatibility.md)。

查詢後完成 Scan，關閉 Rows 並檢查 Err。SQLite 的取消、未掃描 Row、未關閉 cursor、提交／回滾及 RequiresNew 都有真實 Driver 測試；多租戶資格與 Permission 仍由入口及應用政策處理。
