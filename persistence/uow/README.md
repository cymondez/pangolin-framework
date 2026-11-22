# UOW：callback 管理的交易範圍

Application 依賴 [UnitOfWorkManager](contracts.go)，以 `Within` callback 建立業務邊界。Manager 擁有 Commit／Rollback／完成回呼；Repository 只能使用 callback 傳入的 context。實作見 [manager.go](manager.go)，完整安全／可靠事件完成順序仍由 M3 驗收。

```go
manager, err := uow.NewManager(resource, uow.DefaultOptions{
    DefaultTimeout: 5*time.Second, CleanupTimeout: 2*time.Second,
    HookTimeout: time.Second, MaxRequiresNewDepth: 4,
})
// resource 實作 Begin 並返回 Manager 獨占的 Transaction；先檢查 err。
err = manager.Within(trustedContext, uow.Options{Mode: uow.ReadWrite},
    func(work context.Context) error { return repository.Insert(work, book) })
```

頂層需要由可信 ScopeResolver 安裝的 binding；自行填 ScopeBinding struct 不會建立權限。未指定 Mode 為 ReadOnly；TransactionAuto 在 ReadWrite 或 RLS 下使用交易；不依賴 RLS 的唯讀 Resource 可明示非交易。Required 子視圖共用根交易；子失敗／取消／不相容選項使根 rollback-only，即使外層捕捉錯誤也不會提交。唯讀子視圖拒絕寫入，callback 結束後 context 失效。

支援 Required／RequiresNew、ReadCommitted／RepeatableRead／Serializable；非法選項在 Begin 前拒絕。RequiresNew 暫停父根，使用獨立資源並保留父 binding／期限。父子分別提交，不保證整條呼叫鏈原子性。SQL 已直接送入交易，尚無 change tracker；SaveChanges 呼叫可選 Flush，只 flush、不提交或排空事件。Local Event／Audit／冪等完成流程尚未接入，不能據此宣稱完整 §5 已驗收。

callback 返回 nil 後仍需檢查取消／rollback-only 並確認 Commit。Commit 無法判定時保守保存 Unknown，回傳 `UOW_COMMIT_UNKNOWN`，不自動重跑 callback；Rollback 不將 Unknown 改為確定失敗。`errors.As(err, &uow.Error)` 可讀取 Code／ScopeID／OperationID／Outcome，cause 可透過 errors.Is／Unwrap 判斷。

回滾與完成回呼使用獨立有界 context；panic 清理後重拋。OnCompleted／OnFailed／OnDisposed 不取得舊 Executor，回呼錯誤／panic／合作式 timeout 記錄後繼續，不使已提交的 Within 返回業務失敗。hook context 重新建立，不保留舊 UOW／Executor。可靠後續工作必須走 Outbox。

驗證：`go test ./persistence/uow`；真實 PostgreSQL 提交／回滾／RLS 用 [M1 整合測試](../../docs/guides/m1-development.md)，不能以 fake Resource 測試代替。

Resource 由 DI 綁定 DatabaseKey，PostgreSQL 以 Pool 身分預設；同根拒絕第二個 key。可選 Flush／BeforeCommit／Prepare／Release 由 Manager 控制完成順序；內部 completion context 才能在 Completing 使用 Executor。Rollback／Release 清理受 CleanupTimeout 約束，主要 cause 保留。

驗收與邊界見 [M2 指南](../../docs/guides/m2-development.md)，API 變更見 [遷移說明](../../docs/release-notes/m2.md)。
