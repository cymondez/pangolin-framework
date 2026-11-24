# Local Event Bus

`NewBus(Limits, handlers...)` 在啟動時封存 Handler，以 Priority 小者優先、同值依註冊順序執行。`Enqueue(ctx, event)` 僅在目前可寫 UOW 入列，Required 共用根佇列，RequiresNew 由 Resource 建立獨立佇列。

透過 `events/postgres.Resource.Local` 組裝，完成階段 `Queue.Drain` 按 FIFO 迭代新事件，每次 Handler 後 flush；不是遞迴 Go 呼叫。頂層因果深度 0，Handler 新事件 +1；等於配置上限可執行，超過 MaxEventsPerUOW 或 MaxDispatchDepth 使根 rollback-only。設定必須為正數。SaveChanges 不 Drain、不提交。

所有錯誤保留 cause，Panic 由 UOW 清理後重拋。Bus 沒有外部資源，不需關閉。`go test ./events/local` 驗證順序、FIFO、循環與上限；同交易資料／Outbox／Audit 使用 `tests/m3` 真實資料庫驗證。
