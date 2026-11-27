---
title: PostgreSQL Adapter 組裝
description: 在組裝層接上 pgxpool 與 PostgreSQL Adapter，並理解 RLS、最小權限 DB 角色與原生 pgx 擴充邊界。
---

> **你在架構中的位置**：`Composition Root (composition/database.go)` **`[ 建立 pgxpool → 封裝為 &postgres.Resource{} ]`** `→ 傳給 uow.NewManager`
>
> **前置建議**：先完成[中立資料存取與資料庫選擇](database.md)的 `books/repository.go` 與 `NewDatabaseService`。

PostgreSQL 是 panGOlin 功能最完整的首選資料庫（完整支援引擎級 RLS、唯讀交易，以及框架內建的組織治理、Audit、Outbox／Inbox 與 Jobs Adapter）。本頁說明如何把 PostgreSQL 乾淨地接在組裝層，而不讓 `pgx` 型別滲透進 Application。

---

## 1. 在組裝端建立 `database.Resource`

以下完整檔案可放在應用的 `composition/database.go`。連線池建立者負責在應用停機（停止接入、Drain 與 UOW 清理完成）後呼叫回傳的 `cleanup` 函式：

```go
package composition

import (
	"context"

	"github.com/cymondez/pangolin-framework/persistence/database"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OpenPostgresResource 在啟動組裝層建立 pgx 連線池並回傳中立的 database.Resource。
// dsn 應由受信任的啟動設定或密鑰服務提供，絕不接受外部請求動態指定。
func OpenPostgresResource(ctx context.Context, dsn string) (database.Resource, func(), error) {
	// 步驟 1：依啟動設定建立 pgxpool（連線數與逾時可於 DSN 或 pgxpool.ParseConfig 調整）。
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	// 步驟 2：在啟動階段立即 Ping 確認資料庫可達；失敗時立刻關閉連線池避免洩漏。
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, err
	}
	// 步驟 3：回傳實作 database.Resource 的 postgres.Resource 與關閉清理函式。
	return &postgres.Resource{Pool: pool}, pool.Close, nil
}
```

將回傳的 `database.Resource` 直接傳入上一章的 `books.NewDatabaseService(resource)`，整條呼叫鏈就完全接通。

---

## 2. 多租戶與 PostgreSQL RLS 雙重防線

panGOlin 對 PostgreSQL 採「**應用 SQL 條件 + 資料庫 RLS**」雙重隔離，防止任何單一點失誤造成跨租戶資料洩漏：

```text
UOW 開啟交易時 ──▶ 自動執行 transaction-local SET LOCAL（寫入目前 Tenant / ScopeRef）
                          │
                          ├── 防線 1：Repository SQL 帶入完整 owner WHERE 條件
                          └── 防線 2：PostgreSQL FORCE RLS Policy 比對同一組 session 變數
```

### 正式部署必須守住的 3 個 DB 權限鐵律

1. **應用連線帳號（`app_role`）絕不可是 `SUPERUSER`、資料表擁有者（table owner）或具備 `BYPASSRLS`**：否則 PostgreSQL 會直接跳過 RLS Policy，讓第二道防線形同虛設！
2. **所有租戶業務資料表一律啟用 `ENABLE ROW LEVEL SECURITY` 與 `FORCE ROW LEVEL SECURITY`**。
3. **實測驗證隔離**：`resource.Capabilities()` 只代表「PostgreSQL 支援 RLS 機制」，不代表你的資料表已經正確安裝 Policy。部署完 [Migration](migrations.md) 後，務必用實際的 `app_role` 驗證跨租戶讀寫確實被擋下。

> 完整角色拆權與跨租戶設計見[多租戶與資料隔離](../security/multi-tenancy.md)與[資源 Scope 與組織擴充](../security/resource-scopes.md)。

---

## 3. 何時需要原生 `postgres.Current` 與精簡錯誤映射？

- **一般業務 Repository**：統一使用中立的 `database.Current(ctx)` 或 `database.CurrentWithScope(ctx)` 即可。
- **需要 PostgreSQL／pgx 專屬語法（例如 `pgx.Batch`、特定陣列型別或專屬指令）的 Infrastructure Adapter**：可呼叫 `postgres.CurrentWithScope(ctx, write)` 一步取得原生執行器與可信 `scope.ScopeRef`（`write=true` 表示本次要寫入，唯讀 UOW 會直接拒絕），並以 `postgres.MapError(err)` 將 `pgx.ErrNoRows` 與唯一鍵衝突（SQLSTATE `23505`，亦可用 `postgres.IsUniqueViolation(err)` 檢查）映射為中立 `database.ErrNoRows`／`database.ErrConflict`，同時保留原始 driver cause。
- **共用同一個 UOW Session**：原生 `postgres.Current` 與中立 `database.Current` 背後操作的是同一筆交易與同一條連線，因此**同一筆 UOW 內不可平行執行 SQL**（不可藉兩個入口同時下查詢）；若需多執行緒平行操作資料庫，請在各 goroutine 中以 `RequiresNew` 或 `uow.Detach` 開啟各自獨立的 UOW 與連線。

---

## 4. 常見踩坑提醒

- **❌ 查詢多列後忘記 `rows.Close()` 或漏查 `rows.Err()`**：會導致 UOW 判斷連線仍被占用（`ErrBusy`），進而無法 Commit；也絕不可把 `Rows` cursor 帶出 `Within` 回呼外。
- **❌ 在 Repository 內自行 `pool.Begin()`**：繞過 UOW 將導致沒有設定 transaction-local RLS、無法參與 `Required` 回滾、也不會觸發 Outbox／Local Events。

## 下一步

- 需要同時支援其他資料庫？閱讀 **[MySQL Adapter](mysql.md)** 與 **[SQLite Adapter](sqlite.md)**。
- 準備管理資料表結構？前往 **[部署資料庫遷移](migrations.md)**。
