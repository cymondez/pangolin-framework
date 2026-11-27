---
title: MySQL Adapter 組裝
description: 在組裝層接上 go-sql-driver/mysql 與中立 database.Resource，了解 MySQL 的能力邊界與沒有引擎 RLS 時的隔離責任。
---

> **你在架構中的位置**：`Composition Root (composition/mysql.go)` **`[ mysql.Open → database.Resource ]`** `→ 傳給 uow.NewManager`
>
> **前置建議**：先閱讀[中立資料存取與資料庫選擇](database.md)，準備好 `books/repository.go` 與 `NewDatabaseService`。

當你的專案必須使用既有的 MySQL 資料庫時，`persistence/mysql`（底層採用 `go-sql-driver/mysql`）提供與 PostgreSQL 相同的 `database.Resource` 契約，讓 Application 與中立 Repository 完全不必改寫。

---

## 1. 在組裝端建立 MySQL Resource

以下完整檔案可放在應用的 `composition/mysql.go`（`dsn` 須來自可信啟動設定或密鑰服務）：

```go
package composition

import (
	"context"
	"errors"

	"github.com/cymondez/pangolin-framework/persistence/database"
	"github.com/cymondez/pangolin-framework/persistence/mysql"
)

// OpenMySQLResource 建立 MySQL 連線池並回傳中立 database.Resource 與清理函式。
func OpenMySQLResource(ctx context.Context, dsn string) (database.Resource, func() error, error) {
	// 步驟 1：開啟 MySQL Adapter（MaxOpen 至少須 >= 2，才能支援 RequiresNew 巢狀交易）。
	resource, err := mysql.Open(mysql.Config{DSN: dsn, MaxOpen: 4})
	if err != nil {
		return nil, nil, err
	}
	// 步驟 2：啟動前驗證連線可達；失敗時立刻關閉連線池並保留兩個錯誤原因。
	if err = resource.Pool().PingContext(ctx); err != nil {
		return nil, nil, errors.Join(err, resource.Pool().Close())
	}
	return resource, resource.Pool().Close, nil
}
```

> **已有現成 `*sql.DB`？** 如果既有專案已經建立好 `*sql.DB`，可直接改用 `mysql.Wrap(db)` 包裝成 `database.Resource`；連線池的關閉責任仍歸原建立者，須在應用 Drain 與 UOW 清理完成後才關閉。

---

## 2. MySQL 能力與安全隔離責任

### 支援項目
- 隔離級別：`uow.ReadCommitted`、`uow.RepeatableRead`、`uow.Serializable`。
- 引擎級唯讀交易（`ReadOnly`）與獨立連線 `RequiresNew`。
- `Dialect.Placeholder(n)` 自動產生 `?`，不需在 Repository 手動判斷引擎。

### 必須注意的邊界（與 PostgreSQL 的核心差異）

1. **沒有引擎級 RLS**：MySQL 不提供 PostgreSQL 的 Row-Level Security。因此資料隔離**完全仰賴**：
   - Repository 所有 SQL 皆使用 `database.CurrentOwner(ctx)` 帶入完整 `tenant_id`、`owner_scope_kind`、`owner_scope_id` 條件。
   - 資料表 Schema 確實建立含完整 owner 的複合唯一鍵與複合外鍵。
   - 若應用在啟動時呼叫 `database.Admit` 明示要求引擎 RLS，MySQL 會誠實拒絕啟動，絕不假裝支援。
2. **進階平台 Adapter 尚未移植至 MySQL**：框架內建的完整 Bookstore、組織治理、Audit、Outbox／Inbox 與持久化 Jobs 仍為 PostgreSQL 專用 SQL／Migration，**不可只更換 DSN 就視為已支援**，詳見[功能相容性](../reference/compatibility.md)。

---

## 3. 常見執行期注意事項

- 與 PostgreSQL 相同：同一 UOW 內禁止平行下 SQL；`QueryRow` 必須 `Scan`；多列 `Rows` 必須 `Close()` 並檢查 `rows.Err()`。
- 收到 `uow.ErrCommitUnknown` 時，切勿無條件重跑寫入，請透過操作識別或[冪等機制](idempotency.md)對帳。

## 下一步

- 想看免安裝資料庫的輕量方案？閱讀 **[SQLite Adapter 組裝](sqlite.md)**。
- 確認各功能與資料庫的支援矩陣？查看 **[功能相容性](../reference/compatibility.md)**。
