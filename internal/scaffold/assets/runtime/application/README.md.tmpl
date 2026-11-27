# application

`Command(ctx, manager, callback)` 明示 ReadWrite／Transactional；`Query` 明示 ReadOnly。需要回傳值時使用 `CommandResult[T](ctx, manager, callback)`／`QueryResult[T](ctx, manager, callback)`；兩組 API 共用同一完成與觀測 helper。HTTP、Job、Consumer 呼叫同一 Application Use Case，入口方法或反射不會改寫交易政策。直接使用 Manager 的零值選項仍按規範預設 ReadOnly。

```go
book, err := application.QueryResult(ctx, manager, func(work context.Context) (domain.Book, error) {
        return repository.Get(work, id)
})
```

泛型 callback 的值只在 callback 成功且 Manager 確認完成後回傳；Commit 或 rollback-like 錯誤回傳對應零值與原錯誤，不把尚未確認完成的值當成成功結果。

callback 僅使用傳入的 context。UOW 完成階段執行 Local Event／flush 時，標準 Command／Query 可在 Manager 私有完成標記下以 Required 加入根操作；一般重入與 RequiresNew 仍拒絕。返回 nil 是要求完成，仍可能因 Commit／取消／rollback-only 失敗；呼叫方保留 cause 與 Outcome，不自行提交或重跑。資源與清理由 Manager 擁有。本 package 無啟動設定，nil Manager 返回 UOW_OPTIONS_INVALID。

敏感 Use Case 使用 `AuthorizedCommand`／`AuthorizedCommandResult[T]` 與 `AuthorizedQuery`／`AuthorizedQueryResult[T]`：在建立交易前拒絕入口權限，再於 admission 後的交易內重驗 action 與 Resource Policy，直接 Application 呼叫同樣受保護。Authorizer 為中立 Port，由 constructor 注入；未設定或缺可信 Scope 預設拒絕。Feature 開啟不授權。底層 Command／Query 僅表示交易政策，應用不能把它們當作權限檢查。

需要單一 DI 注入點整合交易、授權與可選 Command 冪等時，使用 `NewRunner(manager, authorizer, idempotency)` 搭配泛型 `RunCommand[T](ctx, runner, CommandSpec{...}, work)` 與 `RunQuery[T](ctx, runner, QuerySpec{...}, work)`。`IdempotencyExecutor` 為本套件定義的中立介面（與 `*idempotency.Store` 相容），不引入具體資料庫相依。若指定 `Permission` 但缺少 `Authorizer`，或指定 `IdempotencyOp`／`IdempotencyKey` 但缺少 `Idempotency`，一律 fail-closed 拒絕。

驗證：`go test ./application`。背景工作持久化由 [platform/jobs](../platform/jobs/README.md) 提供；可靠 Consumer 使用相同交易政策與明示服務授權。

## 運維與追蹤整合

Command／Query 會在明確注入的 observability context 建立 Use Case span；未注入時維持無觀測副作用。持久化 Job Worker 已在 platform/jobs 提供，Handler 仍明示交易政策與目前服務授權。
