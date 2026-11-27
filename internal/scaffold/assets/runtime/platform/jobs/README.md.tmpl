# Background Jobs

提供不可變的 Job Type／Payload Schema Version Registry、單次及延後執行工作、有界重試、Deadline、取消與 at-least-once Worker。持久化 Adapter 在 [postgres](postgres/README.md)。產品版本 0.1.0；複雜 Cron 不在目前範圍。

以 `NewRegistry(maxPayloadBytes, Definition{Type, Version, Validate, Handle})` 建立 Registry；重複 Type／Version、缺 Handler／Validator 或非法 payload 都拒絕。已發布版本保持不變，以新增版本處理升級。Job 保存完整 ScopeRef、Actor、Delegated、OperationID、TraceParent、DueAt 與 Deadline。Payload 不授予任何身分或 Scope 權限。

`NewWorker(store, registry, policy, name, bind, checkActor, observer)` 明確注入依賴。Bind 從 Worker 的新 context 建立當前服務身分、核對 Tenant／Scope grant 並授權工作操作；Worker 再比對可信 binding 的完整 owner 和 service principal。Delegated 工作另呼叫 CheckActor 驗證使用者目前資格。需要 UOW 的 Handler 自行選擇 Command／Query Policy，UOW admission 仍重新檢查權威 revision。原使用者僅為 attribution 的已提交工作不自動套用代理語意。

Policy 所有容量、timeout、退避、retention 都必填；Lease 必須長於 ExecutionTimeout 加 CleanupTimeout。Worker 的 `Start()` 建立固定 Concurrency 個迴圈，每個迴圈一次只認領一筆，不預抓無界清單；BatchSize 是認領上限，首版每次一筆。`Process(workerContext)` 供獨立排程器手動驅動，同樣受 Concurrency 限制。外部副作用必須以穩定 Job ID 作冪等鍵，處理成功後崩潰仍可能再次執行。

Worker 實作 Runtime Participant：Quiesce 停止新認領，Drain 等待執行與清理，Cancel 傳遞取消。Handler 必須遵守 context deadline；尚未退出的 Handler 不提前交還 lease。退出後才以獨立 CleanupTimeout 做 token-fenced Finish／Release。Panic 轉成安全失敗，依有界政策重試；不保存 panic 或錯誤原文。超期／取消／失敗／租約遺失分別保留可辨識狀態或錯誤，DB／Broker 仍由組裝擁有者最後關閉。

執行 `go test ./platform/jobs/...`；真實 PostgreSQL、兩 Worker、取消、撤權、dead replay、負載與停機驗收見 [M4 測試](../../tests/m4/README.md)。
