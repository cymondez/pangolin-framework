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

Manager 表達 Required／RequiresNew 與 ReadCommitted／RepeatableRead／Serializable；各 Adapter 只接受自己能保證的隔離級別。Manager 以 `kernel/ambient` 的不可變 Copy-on-Write 巢狀鏈管理 Scope 視圖，RequiresNew 建立獨立根 Scope、獨立連線與交易並保留父 binding／期限，**不修改也不暫停父 Scope**——因此多個 goroutine 可在同一個有效父 UOW 下安全平行呼叫 RequiresNew 共用同一個無狀態 Repository（單一 UOW 內的單一連線仍以 `UOW_BUSY` 防重疊保護）。`uow.Detach(ctx)` 透過 `ambient.Var.Clear` 清除當前 UOW 視圖、完成標記與透過 `uow.AttachSlot` 掛載的資源槽位（`database`／`postgres` Executor、Local／Outbox 佇列），同時保留已驗證的身分／Tenant／ScopeBinding／Deadline，讓平行 goroutine 可啟動重新執行 `scope.Admit` 的獨立頂層 UOW。`ErrSuspended`（`UOW_SUSPENDED`）僅保留作向後相容 sentinel，Manager 不再發出此錯誤。父子分別提交，不保證整條呼叫鏈原子性；連線不足或引擎寫鎖衝突可令獨立子操作失敗。SQL 已直接送入交易，尚無 change tracker；SaveChanges 呼叫可選 Flush，只 flush、不提交或排空事件。Local Event／Audit／冪等完成流程由明示組裝的 Resource 接入，核心 Manager 不自動建立這些持久化 Adapter。

`DefaultOptions.DefaultIsolation` 控制未指定隔離級別的交易；零值維持既有 ReadCommitted。SQLite 組裝需明示 Serializable；也可為三種引擎統一選擇 Serializable。呼叫端明示的 Options.Isolation 優先，後端無法支援時拒絕，不靜默改成別的隔離級別。

callback 返回 nil 後仍需檢查取消／rollback-only 並確認 Commit。Commit 無法判定時保守保存 Unknown，回傳 `UOW_COMMIT_UNKNOWN`，不自動重跑 callback；Rollback 不將 Unknown 改為確定失敗。`errors.As(err, &uow.Error)` 可讀取 Code／ScopeID／OperationID／Outcome，cause 可透過 errors.Is／Unwrap 判斷。

回滾與完成回呼使用獨立有界 context；panic 清理後重拋。OnCompleted／OnFailed／OnDisposed 不取得舊 Executor，回呼錯誤／panic／合作式 timeout 記錄後繼續，不使已提交的 Within 返回業務失敗。hook context 重新建立，不保留舊 UOW／Executor。可靠後續工作必須走 Outbox。

驗證：`go test ./persistence/uow`；真實 PostgreSQL 提交／回滾／RLS 用 [M1 整合測試](../../docs/guides/m1-development.md)，不能以 fake Resource 測試代替。

Resource 由 DI 綁定 DatabaseKey，PostgreSQL 以 Pool 身分預設；同根拒絕第二個 key。可選 Flush／BeforeCommit／Prepare／Release 由 Manager 控制完成順序。完成階段的 Local Event／Flush 可透過標準 Application Command／Query 以 Required 加入同一根 UOW；RequiresNew 與外部重入仍拒絕。巢狀 Required 失敗即使被捕捉仍標記根回滾；已退出的子 view 不得污染仍有效的根。Rollback／Release 清理受 CleanupTimeout 約束，主要 cause 保留。

驗收與邊界見 [M2 指南](../../docs/guides/m2-development.md)，API 變更見 [遷移說明](../../docs/release-notes/m2.md)。

每個新根在取得 DB 資源與 callback 前，透過 `scope.Admit` 重新查驗當前權威，Ref／Principal 不可改寫，只更新一致 revision。Bind 後撤權因此拒絕新操作；Required／RequiresNew 使用父操作已 admission 的 binding／期限。Manager 不解析 Organization 私有資料。`events/postgres.Resource` 可明示組裝 Local／Outbox 完成階段，必要稽核與成功結果仍用同一 Executor。

## 運維與追蹤整合

明確注入 observability context 時，Within 建立 UOW span 與有限 cardinality 的結果／duration Metrics。只記安全錯誤碼，Telemetry 故障不改寫 Commit outcome；未注入時不建立全域 Provider。
