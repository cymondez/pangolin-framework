# PostgreSQL：受控 pgx Executor

此套件實作 [UOW Resource](../uow/README.md)，不向 Application 暴露 Pool、Tx、Begin 或 Commit。Application 只依賴 Repository Port；Infrastructure Adapter 才能 import 本套件。實作見 [resource.go](resource.go)。

`&postgres.Resource{Pool: pool}` 交給 `uow.NewManager`。Pool 由組裝端／DI Factory 擁有並最終關閉；Resource 只管理每次 Within 取得的交易。Begin 驗證可信完整 Scope binding，以明示隔離級別及 ReadOnly 或 ReadWrite 開交易，並使用 `set_config(..., true)` 安裝 transaction-local tenant 與完整 Scope owner。

```go
executor, err := postgres.Current(work, true) // 寫入必須使用有效可寫 Scope
if err != nil { return err }
_, err = executor.Exec(work, query, args...)
```

`Current(work, false)` 用於讀取；GetBook 等 sqlc QueryRow 呼叫也必須傳同一個 work。Exec 一律要求可寫 Scope；沒有 Scope、已結束 context、不同 context 或平行 SQL 都拒絕。QueryRow 直到 Scan 完成才釋放占用，包含延遲錯誤；Repository 必須呼叫 Scan。寫入 RETURNING 的 QueryRow／Query 需以 Current(work,true) 取得 Executor。Commit 前檢查未消耗的 row。Query 支援多列 Rows，直到 Close／讀盡才釋放占用，Scan／Err 的延遲錯誤會使根 rollback-only。Rows.Conn 回傳 nil；沒有公開 Batch／交易控制入口。

RLS 與 schema 權限由部署／fixture 建立；此 Resource 不執行 DDL、不提升 DB role，也不以 Pool 自動提交補救 Scope 缺漏。應用 role 不得是 owner、superuser 或 BYPASSRLS。Bookstore schema 與 sqlc 用法見 [Infrastructure](../../examples/bookstore/infrastructure/README.md)。

驗證：依 [M1 指南](../../docs/guides/m1-development.md) 設定服務後執行 `go test -tags=integration ./tests/m1`，涵蓋真實提交、取消 Begin、第二次 SQL 失敗、RLS、過期 context 與併發拒絕。

已消耗的資料庫錯誤、busy 與資源不相容即使被呼叫端捕捉仍使根回滾。清理不安全的連線以 Hijack／關閉移出 Pool；只有收到 PostgreSQL 明確回滾證據才分類 RolledBack，無法確認的 Commit 保留 Unknown。Resource 不支持非交易 RLS 操作。驗收見 [M2 指南](../../docs/guides/m2-development.md)。
