# SQLite Adapter

以 `modernc.org/sqlite` 的 pure Go Driver 接上中立 `persistence/database` Port 與 UOW，不需要 CGO。SQLite 是實際引擎，不以 MemoryProvider 或 mock 取代資料庫驗收。

`Open(Config{Path: "app.db", MaxOpen: ..., Key: ...})` 開啟檔案資料庫；Memory 明示選用記憶體資料庫。Repository 與其他引擎使用同一個 database.Current／Executor。Pool 僅供擁有組裝與基礎設施使用，業務資料仍走 UOW。

## 隔離與組裝

Manager 的 `DefaultOptions.DefaultIsolation` 明示設為 `uow.Serializable`；明示 ReadCommitted 或 RepeatableRead 會被拒絕。SQLite 使用 `?` Placeholder，不執行 PostgreSQL 專屬 SQL。

SQLite 沒有 PostgreSQL RLS 或可在每次操作切換的引擎唯讀交易。Capabilities 如實回報；要求上述能力的組裝以 database.Admit 拒絕。UOW 仍限制可寫交易、唯讀視圖、同一資源、已結束 context 及重疊 SQL；讀取 API 的 SQL 必須符合其讀取用途。這些規則不冒稱為引擎 RLS。

Repository 必須使用可信 Tenant／完整 Scope 條件及同 owner 複合約束。檔案位置與連線生命週期由應用組裝擁有，不能將外部使用者輸入當成任意資料庫路徑。

## RequiresNew 與清理

RequiresNew 使用獨立連線，不是 Savepoint。先完成的子交易不隨父交易回滾；父交易已持有寫鎖時，另一連線的子寫入可因 SQLite 單一 writer 限制失敗。應用需檢查錯誤，不隱式重試或放寬隔離。

Pool 必須容納使用情境需要的獨立連線；記憶體資料庫還需維持其生命週期。組裝端在操作 Drain 後關閉 Pool。Row／Rows 必須完成 Scan／Close 並檢查延遲錯誤。

SQLite 以真實 Driver 驗證提交／回滾、Required／RequiresNew、owner、每連線外鍵、取消與 cursor 清理，並通過完整 Linux race。來源及重現見 [修正驗收](../../docs/evidence/M5-corrections.md)與 [M5 指南](../../docs/guides/m5-development.md)。
