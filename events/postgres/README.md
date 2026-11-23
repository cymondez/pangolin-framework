# Event PostgreSQL Adapter

`Resource{Base,Local,Publisher,Before}` 包裝原 UOW Resource，保留 DatabaseKey、transaction requirement、Commit outcome 與清理責任。完成順序是 Base flush、Local FIFO／flush、Base BeforeCommit、Outbox Flush、必要 Before callbacks、Prepare、一次 Commit。context／Transaction 不對 Application 暴露。Base 的 Pool 由原擁有者關閉。

`NewStore(queuePool, outbox.Policy)` 實作 Outbox／Inbox Port。業務 Append／Inbox Handler 只用 `persistence/postgres.Current`；專用 queuePool 用於短認領、持久化接收、候選枚舉與失敗後重試。最小 Worker role 僅對佇列表有系統權限，業務表仍受完整 owner RLS。

部署以 `Migrations` 的 `migrations` 子目錄註冊 Goose。schema 為 `pangolin_events`，與舊 Bookstore 薄片分開；啟動 API 不執行 DDL。Policy 所需值全部明示；Store 不擁有 Pool，不自行 Close。`go test ./events/...` 與真實 PostgreSQL／NATS 的 `go test -tags=integration ./tests/m3` 驗證持久化、token、行鎖與對帳。

`NewMaintenance(store, authorize, audit).ReplayDead` 要求已有可信 owner 的可寫 UOW、獨立重送 Permission 與必要 Audit callback；同交易鎖定 dead 項目、寫稽核後重設 pending。它保留 Event ID、完整 Envelope 與 owner，拒絕將 processed 重設為 pending。Audit 失敗使整個重送回滾。

`Store.Purge(ctx, Retention)` 僅由專用 Queue Infrastructure 工作呼叫，清除超過政策的 sent／processed 終態；dead 與 pending 保留。Inbox 保留期必須同時涵蓋 Broker 保留與人工重播窗口，超過期限不宣稱防重。Retry／Complete 依版本或每次不同的 lease token 更新，舊 Worker 不能覆寫新的處理結果；敏感原始錯誤不保存為 last_error。
