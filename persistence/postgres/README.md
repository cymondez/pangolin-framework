# PostgreSQL：受控 pgx Executor

此套件實作 [UOW Resource](../uow/README.md)，不向 Application 暴露 Pool、Tx、Begin 或 Commit。Application 只依賴 Repository Port；Infrastructure Adapter 才能 import 本套件。實作見 [resource.go](resource.go)。

`&postgres.Resource{Pool: pool}` 交給 `uow.NewManager`。Pool 由組裝端／DI Factory 擁有並最終關閉；Resource 只管理每次 Within 取得的交易。Begin 驗證可信 tenant binding，以 ReadCommitted／ReadOnly 或 ReadWrite 開交易，並使用 `set_config(..., true)` 安裝 transaction-local tenant。

```go
executor, err := postgres.Current(work, true) // 寫入必須使用有效可寫 Scope
if err != nil { return err }
_, err = executor.Exec(work, query, args...)
```

`Current(work, false)` 用於讀取；GetBook 等 sqlc QueryRow 呼叫也必須傳同一個 work。Exec 一律要求可寫 Scope；沒有 Scope、已結束 context、不同 context 或平行 SQL 都拒絕。QueryRow 直到 Scan 完成才釋放占用，包含延遲錯誤；Repository 必須呼叫 Scan。Commit 前檢查未消耗的 row。M1 多列 Query 明確不支援，完整 Rows／BatchResults 矩陣留待 M2。

RLS 與 schema 權限由部署／fixture 建立；此 Resource 不執行 DDL、不提升 DB role，也不以 Pool 自動提交補救 Scope 缺漏。應用 role 不得是 owner、superuser 或 BYPASSRLS。Bookstore schema 與 sqlc 用法見 [Infrastructure](../../examples/bookstore/infrastructure/README.md)。

驗證：依 [M1 指南](../../docs/guides/m1-development.md) 設定服務後執行 `go test -tags=integration ./tests/m1`，涵蓋真實提交、取消 Begin、第二次 SQL 失敗、RLS、過期 context 與併發拒絕。
