# 三引擎資料存取契約

這裡的固定 id／value 表格與 fixtureStore 僅供驗收，不是 Framework 的公開業務模型。PostgreSQL 與 MySQL 執行相同中立 Port 的提交／回滾、Required／RequiresNew、owner 條件、DTO owner 拒絕、唯讀、資源身分與 busy cursor 契約。SQLite 真實 Driver 與併發／取消／外鍵案例位於 persistence/database 及 persistence/sqlite 的並置測試。

PostgreSQL fixture 在自己的 pangolin_db_neutral schema 建表、設 FORCE RLS 並授權受限應用角色；使用者不得拿 superuser 連線當應用權限證據。MySQL 使用專用驗收資料庫，Capabilities 明示沒有引擎 RLS。缺少必要服務或環境值時測試失敗，不以 skip 作完成證據。

本機整合入口：`go test -tags=integration ./tests/database`。需要 PANGOLIN_DATABASE_POSTGRES_DSN、PANGOLIN_M1_ADMIN_DSN、PANGOLIN_DATABASE_MYSQL_DSN；只在本機測試程序提供，不把實際憑證交給外部程式代理或輸出到 logs。固定版本與 Compose 接線見 deploy/compose/m5-databases.yaml。

M5 累積 Gate 必須收集這些具名整合結果與並置 SQLite 結果；單獨編譯、mock、未執行的測試或只有文件不能當作交付。正式來源 0d811a21f8b599196f0bad2dd2ee9033d1aa184b 及重現結果見 [修正驗收](../../docs/evidence/M5-corrections.md)與 [M5 指南](../../docs/guides/m5-development.md)。
