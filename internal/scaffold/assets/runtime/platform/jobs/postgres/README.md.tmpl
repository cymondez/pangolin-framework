# PostgreSQL Job Store

`NewStore(queuePool, registry, policy, authorize)` 實作 Jobs queue Port。Queue pool 使用專用 worker role，只在 Infrastructure 列舉／認領／完成工作；Handler 的業務存取仍使用自己的 UOW Executor。Store 不擁有 pool，不關閉外部注入資源。

部署時使用 Goose 將 `Migrations` 的 `migrations/4000001_jobs.sql` 與 Tenant、Scope Catalog、Audit 等來源合併；jobs 依賴 Scope Catalog，API 啟動只檢查 schema，不執行 migration。應用 role 不是 table owner、superuser 或 BYPASSRLS；遷移擁有者必須與應用 role 分離。

`Enqueue(ctx, Job)` 在已授權可寫 UOW 內使用受控 Executor，要求 owner／actor 和 binding 相同。與業務資料一併 Commit／Rollback；全域容量 admission 透過窄化 SECURITY DEFINER function 和 transaction advisory lock 序列化。Function 只回傳是否有容量，不回傳其他 owner 資料。MaxPending 包含保留的全部工作，滿額回 `JOB_CAPACITY_EXCEEDED`，呼叫端回 429 並退避；容量不以單 Tenant 的 RLS 可見筆數估算。

Claim 使用短交易、`FOR UPDATE SKIP LOCKED`、唯一 lease_token 與 attempts 計數；交易外執行 Handler。Finish／Release／取消監測都核對 ID、token、processing 狀態與有效期限；舊 token 回 `JOB_LEASE_LOST`。Document fingerprint 與資料列 owner 不符時隔離為 dead，禁止執行副作用。到期工作及重試耗盡工作轉 dead。

`Cancel`、`Replay` 在目前 owner 的可寫 UOW 內先呼叫 Authorize，狀態與必要 Audit 同交易提交；Audit 失敗必須回滾。Replay 只接受尚未超過 Deadline 的 dead 工作，保留 ID、owner、actor、schema 與 payload。超過 Deadline 必須先對帳，再以明確新操作重新排程，不能把原作業期限偷偷延長。

`Stats` 提供全域聚合，不提供其他 owner payload。`Purge` 只清除超過 Retention 的 succeeded／cancelled，不靜默刪除 dead。操作人員對帳及封存 dead 後才能調整容量；保留窗口內不得為了騰出空間提早刪除冪等證據。

錯誤保留 DB cause；持久化 last_error 只有固定安全碼。Pool 的清理由組裝負責。驗證：`go test -tags=integration ./tests/m4`，連線與完整 Gate 見 [開發指南](../../../docs/guides/m4-development.md)。
