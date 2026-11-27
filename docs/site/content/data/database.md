---
title: 中立資料存取與資料庫選擇
description: 透過 persistence/database 中立執行器與 Dialect，讓同一份 Application 與 Repository 程式碼無痛對應 PostgreSQL、MySQL 或 SQLite。
---

> **你在架構中的位置**：`Application (books/service.go) →` **`[ Infrastructure Repository (persistence/database) ]`** `→ 組裝端挑選具体 Adapter (PostgreSQL / MySQL / SQLite)`
>
> **前置建議**：先閱讀[使用交易與工作單元](unit-of-work.md)，了解 `books/service.go` 定義的 `BookStore` 介面與 `work` context。

## 為什麼需要「中立資料存取」層？

如果 Repository 直接寫死 `pgx.Tx` 或 `*sql.DB`：
- 本機單元測試或輕量工具想改用記憶體 SQLite 跑，幾乎不可能。
- 每個 Repository 方法都得自己處理不同資料庫的佔位符（PostgreSQL 是 `$1, $2`，MySQL／SQLite 是 `?, ?`）。
- 很容易有人不小心直接拿底層 `*sql.DB` 查詢而繞過 UOW 交易與租戶隔離檢查。

panGOlin 在 `persistence/database` 提供一層**極薄且受控的中立抽象**：

```text
[ Application 層 ]   只認識自己定義的 BookStore 介面 + uow.UnitOfWorkManager
        │
        ▼
[ Repository 層 ]    只呼叫 database.Current(work)、database.CurrentOwner(work) 與 Dialect.Placeholder(n)
        │
        ▼
[ Composition Root ] 依部署環境插入具體 database.Resource：
                       ├─ persistence/postgres (pgxpool)
                       ├─ persistence/mysql    (go-sql-driver/mysql)
                       └─ persistence/sqlite   (modernc.org/sqlite, pure Go)
```

三種 Adapter 全部交給同一個 `uow.NewManager`，驅動程式型別（`pgxpool`、`*sql.DB`）完全被隔離在最外層的組裝端。

---

## 1. 三種資料庫 Adapter 快速比較

| 引擎 | 組裝端套件 | 連線設定方式 | SQL 參數佔位符 | 詳細組裝指南 |
|---|---|---|---|---|
| **PostgreSQL** | `persistence/postgres` | `pgxpool.Pool` | `$1`、`$2` | [PostgreSQL Adapter](postgresql.md) |
| **MySQL** | `persistence/mysql` | MySQL DSN／`mysql.Config` | `?` | [MySQL Adapter](mysql.md) |
| **SQLite** | `persistence/sqlite` | 檔案路徑或記憶體模式 | `?` | [SQLite Adapter](sqlite.md) |

---

## 2. 完整中立 Repository 範例：`books/repository.go`

以下完整檔案直接搭配上一章的 `books/service.go`（同屬 `package books`），示範如何實作 `BookStore` 介面並用 `database.Admit` 在啟動前檢查資料庫能力。

> **前置假設**：`books` 資料表由你的應用 Migration 建立，包含 `tenant_id`、`owner_scope_kind`、`owner_scope_id`、`id`、`title` 欄位，且唯一鍵包含完整 owner 與 `id`。

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

// Repository 是實作 BookStore 介面的基礎設施層。
// 注意：struct 裡完全不持有連線池 (*sql.DB / *pgxpool.Pool)！
// 交易執行器一律在每次方法呼叫時，從 UOW 傳入的 ctx 動態取出。
type Repository struct {
	dialect database.Dialect // 負責產生對應資料庫引擎的 SQL 參數佔位符 ($1 或 ?)
}

// Insert 寫入一筆書籍資料。
func (r Repository) Insert(ctx context.Context, title string) error {
	// 步驟 1：從當前 UOW context 取出受控執行器。
	// 若外層忘記開 UOW、UOW 是唯讀、或 UOW 已關閉，這一步會直接回傳錯誤拒絕執行。
	executor, err := database.Current(ctx)
	if err != nil {
		return err
	}
	// 步驟 2：從受信任的 Scope context 取出完整 owner (TenantID, ScopeKind, ScopeID)。
	// 絕對不要從外部請求 DTO 讀取 tenant_id 塞進 SQL！
	owner, err := database.CurrentOwner(ctx)
	if err != nil {
		return err
	}
	// 步驟 3：使用 Dialect 產生佔位符，讓同一份參數化 SQL 可同時跑在 PostgreSQL、MySQL 與 SQLite。
	query := fmt.Sprintf(
		"INSERT INTO books (tenant_id, owner_scope_kind, owner_scope_id, id, title) VALUES (%s,%s,%s,%s,%s)",
		r.dialect.Placeholder(1), r.dialect.Placeholder(2),
		r.dialect.Placeholder(3), r.dialect.Placeholder(4), r.dialect.Placeholder(5),
	)
	_, err = executor.Exec(ctx, query, owner.TenantID, owner.ScopeKind, owner.ScopeID, rand.Text(), title)
	return err
}

// GetTitle 依完整 owner 與書籍 ID 查詢書名。
func (r Repository) GetTitle(ctx context.Context, id string) (string, error) {
	executor, err := database.Current(ctx)
	if err != nil {
		return "", err
	}
	owner, err := database.CurrentOwner(ctx)
	if err != nil {
		return "", err
	}
	// 即使 PostgreSQL 已有 RLS，Repository SQL 依然明示完整 owner 等值條件，形成雙重防線；
	// 切到沒有引擎 RLS 的 MySQL／SQLite 時，這道條件更是核心隔離保障。
	query := fmt.Sprintf(
		"SELECT title FROM books WHERE tenant_id=%s AND owner_scope_kind=%s AND owner_scope_id=%s AND id=%s",
		r.dialect.Placeholder(1), r.dialect.Placeholder(2),
		r.dialect.Placeholder(3), r.dialect.Placeholder(4),
	)
	var title string
	err = executor.QueryRow(ctx, query, owner.TenantID, owner.ScopeKind, owner.ScopeID, id).Scan(&title)
	return title, err
}

// NewDatabaseService 是給組裝層呼叫的工廠函式，將任意 database.Resource 接上 UOW 與 Service。
func NewDatabaseService(resource database.Resource) (*Service, error) {
	if resource == nil {
		return nil, fmt.Errorf("database resource is required")
	}
	// 步驟 1：啟動前能力准入檢查（database.Admit）。
	// 這裡明示要求三種引擎皆支援的 Serializable 隔離級別；若未來應用需要引擎級 RLS，
	// 也可在此宣告，讓不支援的資料庫在啟動第 0 秒就失敗，絕不偷偷降級。
	if err := database.Admit(resource.Capabilities(), database.Requirements{
		Isolation: uow.Serializable,
	}); err != nil {
		return nil, err
	}
	// 步驟 2：建立 UOW Manager（SQLite 組裝須明示 DefaultIsolation: uow.Serializable；
	// 若只用 PostgreSQL/MySQL 且省略 DefaultIsolation，預設為 ReadCommitted）。
	manager, err := uow.NewManager(resource, uow.DefaultOptions{
		DefaultTimeout:      5 * time.Second,
		CleanupTimeout:      2 * time.Second,
		HookTimeout:         time.Second,
		MaxRequiresNewDepth: 4,
		DefaultIsolation:    uow.Serializable,
	})
	if err != nil {
		return nil, err
	}
	// 步驟 3：回傳組裝好的 Application Service。
	return NewService(manager, Repository{dialect: resource.Dialect()}), nil
}
```

### 逐段導讀：三個關鍵設計因果

1. **為什麼 `Repository` 裡沒有保存 `*sql.DB` 或 `*pgxpool.Pool`？**
   如果 Repository 拿得到連線池，未來某個工程師就可能在趕進度時寫出 `r.pool.Exec(...)` 繞過 UOW 與交易管理。只透過 `database.Current(ctx)` 取得執行器，從結構上保證**沒有有效 UOW 就連一行 SQL 都送不出去**。
2. **為什麼要呼叫 `database.CurrentOwner(ctx)`，而不是讓 Service 把 `tenantID` 當字串參數傳進來？**
   字串參數很容易被傳錯來源（例如不小心把 HTTP JSON body 裡的欄位傳進來）。`CurrentOwner(ctx)` 只認經過驗證與綁定的不可變 Scope，杜絕身分偽造。
3. **為什麼要用 `database.Admit(...)` 做能力檢查？**
   不同資料庫的能力天差地遠（見下表）。panGOlin 反對「默默降級假裝有支援」——如果應用聲明必須具備某項隔離或 RLS 能力而所選引擎沒有，啟動前立即拒絕。

---

## 3. 誠實面對引擎差異（不以靜默降級粉飾）

| 能力項目 | PostgreSQL | MySQL | SQLite |
|---|---|---|---|
| **引擎級 RLS（Row-Level Security）** | ✅ 支援（資料表仍須正確設 `FORCE RLS`） | ❌ 不提供（仰賴 Repository owner 條件與 Schema 約束） | ❌ 不提供（仰賴 Repository owner 條件與 Schema 約束） |
| **引擎級唯讀交易** | ✅ 支援 | ✅ 支援 | ❌ 不提供操作級切換（由 UOW 層攔截宣告的寫入） |
| **`RequiresNew` 傳播** | ✅ 獨立連線與獨立交易 | ✅ 獨立連線與獨立交易 | ⚠️ 獨立連線；受限單一 writer，父持寫鎖時子寫入可能失敗 |
| **支援隔離級別** | ReadCommitted / RepeatableRead / Serializable | ReadCommitted / RepeatableRead / Serializable | 僅 `Serializable`（須明示設定） |

> **0.1.0 範圍邊界提醒**：三引擎共用的是「**中立資料存取與 UOW**」。框架內既有完整的 Bookstore 示範、組織治理（Organization）、Audit、可靠事件（Outbox／Inbox）與持久化 Jobs 等 Adapter，目前仍使用 PostgreSQL 專用 SQL 與 Migration——**不能只改 DSN 就當成這些進階模組已支援 MySQL 或 SQLite**。

---

## 4. 錯誤處理、Cursor 清理與資源關閉

- **查無資料判斷**：統一用 `errors.Is(err, database.ErrNoRows)` 判斷，不直接比對各 Driver 自己的 `sql.ErrNoRows` 或 `pgx.ErrNoRows`；其他資料庫錯誤會完整保留底層 Driver cause。
- **Rows／Row 必須徹底收尾**：呼叫 `Query` 取得多列 `Rows` 後，務必 `defer rows.Close()` 並在迴圈結束後檢查 `rows.Err()`；呼叫 `QueryRow` 必須呼叫 `Scan`。未關閉的 `Rows` 會被 UOW 視為資源仍被占用（`ErrBusy`）而阻止正常提交。
- **連線池生命週期**：連線池一律由建立它的組裝端擁有，並在應用停機的 `Quiesce → Drain → UOW Cleanup` 全數完成後才關閉。

## 下一步

依你專案採用的資料庫挑選對應組裝指南：
- **[PostgreSQL Adapter 組裝](postgresql.md)**（支援完整 RLS 與所有進階平台 Adapter）
- **[MySQL Adapter 組裝](mysql.md)**
- **[SQLite Adapter 組裝](sqlite.md)**（免安裝資料庫，適合輕量服務與本機整合測試）
- 接著閱讀 **[Command 冪等與結果對帳](idempotency.md)** 與 **[部署資料庫遷移](migrations.md)**。
