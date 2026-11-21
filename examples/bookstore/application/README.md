# Book Application

`NewBooks(manager, repository, publisher)` 只依賴 Domain、UOW、Scope 與事件 Port。實作見 [books.go](books.go)；具體 pgx／NATS／HTTP Adapter 都在組裝端注入。

`Create(ctx, id, title)` 要求可信 tenant binding。id 未提供時產生新識別；先以 Domain 建構 Book，再以明示 ReadWrite／Required 執行 Insert 與 Publish。`bookstore.book_created` v1 保存完整 owner 與 Book payload，可靠 publisher 必須在同一交易寫 Outbox。任一步驟失敗不返回成功，只有 Within 確認 Commit 後才返回結果。

`Get(ctx, id)` 以 ReadOnly UOW 讀取 Repository。直接 Application 呼叫也必須先經可信 ScopeResolver Bind，不能藉略過 HTTP 省略 Scope。M1 的完整 Permission／Resource Policy／membership 撤權尚未交付。

Create 的 client-supplied id 可用於後續 GET 狀態查詢；目前不提供正式 Idempotency-Key 契約。重複 ID 是 conflict，查無 ID 不能將 Commit Unknown 解釋為確定失敗。

驗證：依 [M1 指南](../../../docs/guides/m1-development.md) 跑 `go test -tags=integration ./tests/m1`，核對真實提交、回滾、owner 與 Outbox。HTTP 成功回應 barrier 另見 Transport 測試。
