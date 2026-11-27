---
title: SQLite Adapter 組裝
description: 在組裝層接上純 Go SQLite（檔案或記憶體模式），理解 Serializable 強制要求與單一 writer 限制。
---

> **你在架構中的位置**：`Composition Root (composition/sqlite.go)` **`[ sqlite.Open → database.Resource ]`** `→ 傳給 uow.NewManager（明示 Serializable）`
>
> **前置建議**：先閱讀[中立資料存取與資料庫選擇](database.md)，準備好 `books/repository.go` 與 `NewDatabaseService`。

`persistence/sqlite` 底層使用 pure Go 的 `modernc.org/sqlite` 驅動程式，**不需要安裝任何資料庫伺服器，也不需要 CGO**，非常適合單機工具、邊緣節點服務或快速本機整合測試，同時保留完整的 panGOlin UOW 與中立 Repository 寫法。

---

## 1. 在組裝端建立 SQLite Resource

以下完整檔案可放在應用的 `composition/sqlite.go`（資料檔路徑應來自受信任的啟動設定，切勿直接取自外部請求參數）：

```go
package composition

import (
	"context"
	"errors"

	"github.com/pangolin-framework/pangolin/persistence/database"
	"github.com/pangolin-framework/pangolin/persistence/sqlite"
)

// OpenSQLiteResource 開啟 SQLite 檔案資料庫並回傳中立 database.Resource 與清理函式。
// path 例如 "./data/app.db"；若要改用測試用記憶體資料庫，可在 Config 改設 Memory: true。
func OpenSQLiteResource(ctx context.Context, path string) (database.Resource, func() error, error) {
	// 步驟 1：開啟 SQLite Adapter；框架會自動在每個連線啟用外鍵檢查（PRAGMA foreign_keys = ON）。
	resource, err := sqlite.Open(sqlite.Config{Path: path, MaxOpen: 4})
	if err != nil {
		return nil, nil, err
	}
	// 步驟 2：啟動前確認檔案可讀寫並可正常回應查詢。
	if err = resource.Pool().PingContext(ctx); err != nil {
		return nil, nil, errors.Join(err, resource.Pool().Close())
	}
	return resource, resource.Pool().Close, nil
}
```

> **記憶體模式提醒**：設定 `sqlite.Config{Memory: true}` 可建立純記憶體 SQLite，但資料存活期受連線池生命週期影響，**僅適合短生命週期測試，不可作為跨程序或持久化儲存**。

---

## 2. 使用 SQLite 必知的 3 個重要特性

1. **必須明示 `DefaultIsolation: uow.Serializable`**
   - SQLite 的交易隔離本質為 Serializable。panGOlin 拒絕默默轉換隔離級別——如果你沒在 `uow.DefaultOptions` 明示 `uow.Serializable`（或在單次 `Within` 指定了 `ReadCommitted`／`RepeatableRead`），Begin 階段會直接回錯拒絕。
   - 上一章[中立資料存取](database.md)的 `NewDatabaseService` 已示範正確設定方式。
2. **`RequiresNew` 與單一 Writer 鎖限制**
   - `Required` 會正常共用同一個根交易；`RequiresNew` 會開啟第二條連線與獨立交易（先提交的子交易不隨父回滾）。
   - 但因 SQLite 同一時間只允許一個 writer，**若父交易已經持有寫鎖，內層 `RequiresNew` 又試圖寫入，將會因鎖衝突失敗**。panGOlin 會忠實回報錯誤，不會偷改成 Savepoint 或隱式重試。
3. **沒有引擎級 RLS 與引擎唯讀交易**
   - SQLite 的唯讀保護由 UOW 層攔截宣告的寫入操作；資料隔離則仰賴 Repository 帶入 `database.CurrentOwner(ctx)` 條件，以及資料表自身的複合唯一鍵與外鍵約束（Adapter 已在每條連線自動開啟外鍵檢查）。
   - 同樣地，框架既有的完整 Bookstore、組織治理、Audit、Outbox／Inbox 與 Jobs 仍為 PostgreSQL 專用 Adapter，詳見[功能相容性](../reference/compatibility.md)。

---

## 3. 常見執行期注意事項

- 與其他引擎一致：`QueryRow` 必須 `Scan`、多列 `Rows` 必須 `Close()` 並檢查 `rows.Err()`；同一筆 UOW（單一連線）不可平行執行 SQL（平行讀取可透過各 goroutine 開立獨立 `RequiresNew` 或 `uow.Detach` UOW 共用同一個無狀態 Repository；平行寫入仍受 SQLite 單一 writer 限制）。
- 多租戶資格驗證與 Permission 檢查仍由入口與應用政策負責，不會因為換用 SQLite 而放寬。

## 下一步

- 準備了解如何防護重複扣款與 `UOW_COMMIT_UNKNOWN`？前往 **[Command 冪等與結果對帳](idempotency.md)**。
