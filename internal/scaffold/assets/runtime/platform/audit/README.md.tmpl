# Audit

`Recorder.Record(ctx, Record)` 在目前可寫 UOW 的 PostgreSQL Executor 保存 Actor、完整 owner、Action、Resource、Result、Trace、Duration。必填必要稽核時，任何錯誤都需向外回傳，使業務、Audit 與 Outbox 一起回滾。部署以 `Migrations` 內 `migrations` 子目錄安裝資料表與 FORCE RLS，保留政策由部署設定。

`NewSecurityLog(writer).Write(ctx, record)` 直接輸出結構化 JSON，獨立於業務交易，即使 request 取消或交易回滾仍可寫入。呼叫者處理 writer 錯誤及告警；這不是可靠背景工作。Record 不提供任意 payload／原始錯誤欄位，識別欄位限長並拒絕空白／換行，不記錄 Token、憑證、SQL 或 request body。Principal ID 應採系統不透明識別碼。

本套件不關閉 injected writer／Pool，由組裝擁有者清理。`go test ./platform/audit`；原子提交／回滾／故障以 `tests/m3` 真實 PostgreSQL 驗收。非必要 Telemetry callback 由 UOW observer 隔離，不翻轉已確認 Commit。

在 Scope 尚未成功選取的拒絕路徑，以 `NewSecurityLog(writer, trustedIdentityReader)` 保留已驗證 Actor；沒有 binding 時不把 header／route hint 當作可信 owner。未驗證身分仍可記錄匿名拒絕。HTTP Pipeline 每階段保留最新可信 context，SecurityLog 的 writer／identity 只由組裝注入。
