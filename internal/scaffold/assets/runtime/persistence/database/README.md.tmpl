# 中立資料存取

本套件定義與資料庫引擎無關的 Executor、Row／Rows、ExecResult、Resource、Dialect 與能力檢查。Executor 寫入回傳 API 的遷移方式見 [MIGRATION.md](MIGRATION.md)。Repository 依賴這個 Port；PostgreSQL、MySQL、SQLite 的連線與 SQL 差異由組裝端及 Adapter 提供。公開 Port 不使用 pgx 或特定 Driver 的型別。

## 在 UOW 中使用

由 `uow.NewManager` 管理交易。Repository 僅使用 Within callback 收到的 context，以 `database.Current(ctx)` 取得目前 Executor。Exec、QueryRowWrite 與 QueryWrite 代表明示寫入意圖，必須處於可寫交易；Query／QueryRow 只接受讀取 SQL，這是呼叫端契約，不是任意 SQL 分類器或 SQLite 的引擎唯讀沙箱。不要把 INSERT／UPDATE／DELETE RETURNING 傳給讀取方法。Executor 不提供 Begin、Commit、Rollback 或原始連線。

ExecResult.RowsAffected 是欄位。QueryRowWrite 用於單列寫入回傳；QueryWrite 用於多列寫入回傳。兩者均先執行 UOW 寫入檢查。QueryRow 的錯誤在 Scan 回報，查無資料可用 `errors.Is(err, database.ErrNoRows)` 判斷。Rows 使用後必須 Close 並檢查 Err；未釋放的 cursor 也會占用 Executor，不能與其他 SQL 重疊或帶離工作範圍。

`database.CurrentOwner(ctx)` 從可信 Scope binding 取得 TenantID、ScopeKind、ScopeID；`ResolveOwner` 驗證外部 owner hint，不能藉 DTO 改寫 owner。Repository 的每個受保護查詢都必須帶完整 owner 等值條件，唯一鍵與需要同 owner 的關聯亦包含完整 owner。這些 helper 不授予權限。

## 組裝與能力

| 引擎 | Adapter | Placeholder | 引擎 RLS | 引擎唯讀交易 |
|---|---|---|---|---|
| PostgreSQL | `persistence/postgres`，重用既有 pgx UOW Adapter | `$1`、`$2` | 有，資料表仍須正確啟用 FORCE RLS | 有 |
| MySQL | `persistence/mysql` | `?` | 無 | 有 |
| SQLite | `persistence/sqlite`，pure Go Driver | `?` | 無 | 無；只對明示寫入 API 強制 UOW 寫入模式 |

SQL Placeholder 及識別字 quoting 使用 Adapter 的 Dialect；不把 PostgreSQL SQL 以字串替換冒充其他引擎。SQL schema、索引與原生功能仍需按引擎選擇，不能直接移用 PostgreSQL migration。

啟動前以 `database.Admit(resource.Capabilities(), requirements)` 檢查必要能力；要求引擎 RLS 或引擎唯讀交易而後端不具備時，必須拒絕組裝。能力表不代表表格已配置正確，也不取代目前身分／Scope 授權。既有依賴 FORCE RLS 的共享 PostgreSQL Profile 保持其保證，不能靜默換成 MySQL／SQLite。

## 錯誤、清理與驗證

資源不符、無 UOW、唯讀寫入、重疊 SQL 與已結束 context 遵守 uow 契約。錯誤保留 cause；無法確認的 Commit 不可聲稱已回滾。只有 Manager 完成交易，Pool 的最終關閉由建立它的組裝端負責。

SQLite 使用真實 Driver，PostgreSQL／MySQL 使用實際服務；三引擎與完整累積 Gate 已於來源 0d811a21f8b599196f0bad2dd2ee9033d1aa184b 通過，見 [驗收證據](../../docs/evidence/M5-corrections.md)與 [M5 重現指南](../../docs/guides/m5-development.md)。
