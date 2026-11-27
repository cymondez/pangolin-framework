# Event PostgreSQL Adapter

`Resource{Base,Local,Publisher,Before}` 包裝原 UOW Resource，保留 DatabaseKey、transaction requirement、Commit outcome 與清理責任。完成順序是 Base flush、Local FIFO／flush、Base BeforeCommit、Outbox Flush、必要 Before callbacks、Prepare、一次 Commit。context／Transaction 不對 Application 暴露。Base 的 Pool 由原擁有者關閉。

`NewStore(queuePool, outbox.Policy)` 實作 Outbox／Inbox Port。業務 Append／Inbox Handler 只用 `persistence/postgres.Current`；專用 queuePool 用於短認領、持久化接收、候選枚舉與失敗後重試。最小 Worker role 僅對佇列表有系統權限，業務表仍受完整 owner RLS。

部署以 `Migrations` 的 `migrations` 子目錄註冊 Goose。schema 為 `pangolin_events`，與舊 Bookstore 薄片分開；啟動 API 不執行 DDL。Policy 所需值全部明示；Store 不擁有 Pool，不自行 Close。`go test ./events/...` 與真實 PostgreSQL／NATS 的 `go test -tags=integration ./tests/m3` 驗證持久化、token、行鎖與對帳。

`NewMaintenance(store, authorize, audit).ReplayDead` 要求已有可信 owner 的可寫 UOW、獨立重送 Permission 與必要 Audit callback；同交易鎖定 dead 項目、寫稽核後重設 pending。它保留 Event ID、完整 Envelope 與 owner，拒絕將 processed 重設為 pending。Audit 失敗使整個重送回滾。

`Store.Purge(ctx, Retention)` 僅由專用 Queue Infrastructure 工作呼叫，清除超過政策的 sent／processed 終態；dead 與 pending 保留。Inbox 保留期必須同時涵蓋 Broker 保留與人工重播窗口，超過期限不宣稱防重。Retry／Complete 依版本或每次不同的 lease token 更新，舊 Worker 不能覆寫新的處理結果；敏感原始錯誤不保存為 last_error。

## 運維與追蹤整合

NewBoundedStore(pool, policy, Capacity{Outbox, Inbox}) 啟用部署授權的資料庫容量 admission，部署時合併 `CapacityMigrations` 的 capacity_migrations 目錄。容量由預配置 permit slots 承載：INSERT 由資料庫觸發器在同一交易認領一個 free slot（`FOR UPDATE SKIP LOCKED`，無全域 advisory lock、無全表計數），DELETE 釋放 slot；保留終態因此仍計入容量。部署以狹化 SECURITY DEFINER `ProvisionQueues(ctx, *sql.DB, outboxCapacity, inboxCapacity, action)` 一次交易安裝／調整兩個佇列的 permit pool：lock_timeout 保護下鎖定佇列與 permit 表、回填採用既有資料列、低於現有列數時拒絕；action `install` 已存在時拒絕，`resize` 重複執行為冪等部署入口。呼叫端組態與部署組態在 admission 同一敘述以索引 config 查核對，不一致、未配置或舊 admit 函式呼叫分別回 EVENT_CAPACITY_CONFIG_CONFLICT／EVENT_CAPACITY_UNPROVISIONED／EVENT_CAPACITY_REQUIRED／EVENT_CAPACITY_UPGRADE_REQUIRED（503 族）與 EVENT_CAPACITY_EXCEEDED（429）；合法 Inbox 重複不需容量仍可 ACK。容量包含保留終態，超限以 429 退避拒絕且不靜默擴容。Stats 回傳 pending／processing／dead／oldest 聚合，配合 observability.Queue 告警。舊 NewStore 保留既有組裝相容性，但對已 provision 佇列 fail-closed；需要有界容量的部署必須選 NewBoundedStore 並在部署完成 provisioning。
