# MySQL Adapter

以 `github.com/go-sql-driver/mysql` 接上中立 `persistence/database` Port 與同一個 UOW Manager。驗收使用真實 MySQL 8.4；Driver 固定為 go.mod 指定版本。

`Open(Config{DSN: ..., MaxOpen: ..., Key: ...})` 建立連線池；也可用 DSNConfig 提供已解析的 Driver 設定。Open 不代表資料庫已可連線，組裝端應以 Pool().PingContext 檢查啟動條件。`Wrap(db, key)` 接用既有 sql.DB，由原建立者擁有其生命週期。

將 Resource 交給 `uow.NewManager`，Repository 從 callback context 呼叫 `database.Current`。Exec／Row／Rows 不暴露 MySQL 型別。DDL、migration、健康檢查可由基礎設施使用 Pool；業務 Repository 不得繞過 UOW 使用它。

## 能力與隔離

支援 ReadCommitted、RepeatableRead、Serializable 與引擎唯讀交易。不支援 PostgreSQL RLS；組裝若要求 RowLevelSecurity，database.Admit 必須回錯誤。所有受保護 SQL 仍以可信 Tenant／完整 Scope 等值限制，並建立同 owner 唯一鍵與外鍵。

SQL 使用 `?` Placeholder。不能直接執行 PostgreSQL 的 set_config、RLS policy 或專屬 migration。新 Port 的三引擎支援不代表既有 PostgreSQL 專用的 Jobs／事件／治理 Adapter 已自動完成移植。

## 錯誤與清理

保留 Driver cause 與 UOW Outcome；未確認的 Commit 回報 Unknown，不由此 Adapter 重試 Use Case。建立連線池的組裝端在 Drain／UOW 清理結束後關閉 Pool，只有一個關閉責任者。

真實 MySQL 的交易、owner 隔離與獨立連線對帳已通過；單元測試不代替此證據。來源及完整重現見 [修正驗收](../../docs/evidence/M5-corrections.md)與 [M5 指南](../../docs/guides/m5-development.md)。
