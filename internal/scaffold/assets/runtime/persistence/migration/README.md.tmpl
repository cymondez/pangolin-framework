# Goose 部署遷移

`NewPostgres(sqlDB, fs, minimum, maximum)` 實作部署 Runner；`Apply` 驗證模組 DAG、缺漏／重複版本與版本順序，再以 Goose v3.30.0 執行。所有模組共用 `pangolin.goose_db_version` 及 `LockKey=0x70616e676f6c696e` 的 session advisory lock。JoinFS 可掛載各 package 的 embedded migration。

`PostgresLocker.Acquire` 的 Lock 保有獨占 SQL session，Release 同 session 解鎖。取消不略過有界清理；無法確認鎖狀態就丟棄連線，不交回可能持鎖的 Pool。Injected sql.DB 由部署組裝關閉。

`Apply` 使用 Goose `PostgresSessionLocker`／`WithLockID` 保護整個部署操作，包含 `Up` 在正式執行前的 `HasPending` 與版本表初始化。Provider 不再重複取得相同鎖；遷移 SQL 使用另一條部署連線，因此 Pool 必須允許至少兩條連線（未設定最大值亦可）。各模組 SQL 檔名版本在整個應用中唯一；相依模組的最大版本必須小於使用者模組的最小版本，缺漏、循環、重複或倒序會在任何 DDL 前拒絕。

`CheckCompatibility` 只讀版本範圍，不執行遷移，供 API Ready 前使用。遷移預設向前修復，不能以啟動 API 自動升級。`ScopeMigrations` 提供中立 `pangolin.resource_scopes` catalog，tenant-only 應用不載入組織 schema。

驗證：`go test ./persistence/migration` 及 `go test -tags=integration ./tests/m3`；雙部署與中斷復原必須使用真實 PostgreSQL，單純 SQL 字串測試不代表資料庫驗收完成。
