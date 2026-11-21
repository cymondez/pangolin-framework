# HTTP：Bookstore 非串流入口

本套件依 `BookService`／`Authenticator`／ScopeResolver Port 呼叫 Application，不直接操作 PostgreSQL 或 Broker。實作見 [books.go](books.go)，組裝見 [bootstrap](../../examples/bookstore/bootstrap/README.md)。

提供 `GET /readyz`、`POST /books`（id 可省略、title 必填）及 `GET /books/<id>`。業務請求在 Host Ready 且 WorkGroup 接受 admission 後，依序建立有界操作、驗證 Bearer JWT、檢查 Tenant／Scope hint、Bind、解碼／驗證，再呼叫 Application。M1 拒絕非 tenant 的 Scope Kind，以及和身分不一致的 Tenant／Scope ID。

POST body 上限 1 MiB，拒絕未知欄位、非法或尾隨 JSON；請求 timeout 為 5s。只有 Application 返回已確認提交的成功結果後才寫入 201／序列化 JSON。提交後傳輸失敗仍可能已寫入，客戶端應保存操作 ID 並查詢狀態；查無結果不能證明 Unknown 已回滾。

錯誤採 `application/problem+json`，包含 type／title／status／detail／instance／traceId／code，公開 detail 不輸出底層 SQL／DSN／Token。M1 映射 400、401、403、404、409、422、503 與一般 500；UOW Unknown 使用 `UOW_COMMIT_UNKNOWN`。完整 Permission／Resource Policy、Trace／Rate Limit 與 API-002 矩陣仍由後續里程碑交付。

Handler 的 WorkGroup 記錄活躍操作；Quiesce 後拒絕新工作。HTTP server 的 listener／Shutdown 必須由 Runtime Participant 管理，不能只停 Ready 後立即關 DB。

驗證：`go test ./transport/http`，涵蓋拒絕順序、body、錯誤遮罩與成功回應 barrier；真實 Commit 前不回 2xx、簽章／租戶拒絕及停機用 [M1 E2E](../../docs/guides/m1-development.md)。Streaming／SSE 尚未交付。
