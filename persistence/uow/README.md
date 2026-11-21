# UOW：M1 Required 交易薄片

Application 依賴 [UnitOfWorkManager](contracts.go)，以 `Within` callback 建立業務邊界。Manager 擁有 Commit／Rollback／完成回呼；Repository 只能使用 callback 傳入的 context。M1 實作見 [manager.go](manager.go)，完整選項矩陣仍由 M2／M3 交付。

```go
manager, err := uow.NewManager(resource, uow.DefaultOptions{
    DefaultTimeout: 5*time.Second, CleanupTimeout: 2*time.Second,
    HookTimeout: time.Second, MaxRequiresNewDepth: 4,
})
// resource 實作 Begin 並返回 Manager 獨占的 Transaction；先檢查 err。
err = manager.Within(trustedContext, uow.Options{Mode: uow.ReadWrite},
    func(work context.Context) error { return repository.Insert(work, book) })
```

頂層需要由可信 ScopeResolver 安裝的 binding；自行填 ScopeBinding struct 不會建立權限。未指定 Mode 為 ReadOnly；ReadOnly／ReadWrite 都使用交易與 transaction-local RLS。Required 子視圖共用根交易；子失敗／取消／不相容選項使根 rollback-only，即使外層捕捉錯誤也不會提交。唯讀子視圖拒絕寫入，callback 結束後 context 失效。

M1 明確拒絕 RequiresNew、NonTransactional、RepeatableRead／Serializable 及未知選項，不靜默降級。SQL 已直接送入交易，尚無 change tracker；目前 SaveChanges 確認 Scope 有效，不提交。Local Event／Audit／冪等完成流程尚未接入，不能據此宣稱完整 §5 已驗收。

callback 返回 nil 後仍需檢查取消／rollback-only 並確認 Commit。Commit 回傳錯誤時保守保存 Unknown，回傳 `UOW_COMMIT_UNKNOWN`，不自動重跑 callback；Rollback 不將 Unknown 改為確定失敗。`errors.As(err, &uow.Error)` 可讀取 Code／ScopeID／OperationID／Outcome，cause 可透過 errors.Is／Unwrap 判斷。

回滾與完成回呼使用獨立有界 context；panic 清理後重拋。OnCompleted／OnFailed／OnDisposed 不取得舊 Executor，回呼錯誤不改寫已確認的 Committed。可靠後續工作必須走 Outbox。

驗證：`go test ./persistence/uow`；真實 PostgreSQL 提交／回滾／RLS 用 [M1 整合測試](../../docs/guides/m1-development.md)，不能以 fake Resource 測試代替。
