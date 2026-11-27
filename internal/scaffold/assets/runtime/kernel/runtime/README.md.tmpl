# Runtime：啟動、Ready 與 Drain

Runtime 以 Module 與 DI Port 組裝應用，不依賴 Bookstore 或組織模型。入口為 `New(options, sources, participants...)`，回傳 [Host](contracts.go)；實作見 [host.go](host.go)、[work.go](work.go)。

啟動依序執行：驗證全部 Module 圖 → 全部 PreConfigure → 全部 Configure → 全部 PostConfigure → 驗證 Config／Seal DI → 依拓撲順序 Start → Ready。每階段跨全部模組執行。任何錯誤不報 Ready；已啟動工作先停止接入與 Drain，再反序 Stop／清理。失敗的模組自行回收未交付的部分資源。

Options 的 DrainTimeout／CleanupTimeout 必須為正值；Source 必須含 [Config 的必要 UOW 設定](../config/README.md)。Host 為一次性物件，失敗或停止後另建新 Host，不重啟同一個物件。`Ready()` 可安全並行讀取。

HTTP 與 Worker 透過 `Participant` 接入停機：Quiesce 先拒絕新請求／認領，Drain 等待已取得的工作，Cancel 在期限到時取消工作；這些方法必須尊重 deadline。所有 Participant 都先 Quiesce，再進入 Drain。之後以有效的清理 context 反序 Stop，最後關閉 DI Container；DB／Broker 不得提早關閉。

```go
group := runtime.NewWorkGroup()
work, done, err := group.Enter(requestContext)
if err != nil { return err }
defer done() // 必須在 Tx／cursor／lease 釋放後才呼叫
// 將 work 傳給這次操作。
```

`WorkGroup` 實作 Participant；Quiesce 後拒絕 Enter，Cancel 取消全部活躍 context。HTTP listener 的 Shutdown 與 Worker 認領迴圈仍須由 Adapter 接到 Participant。實際接法見 [Bookstore bootstrap](../../examples/bookstore/bootstrap/README.md)。

驗證：`go test ./kernel/runtime`。A02／A15 涵蓋全階段順序、Start 失敗回收、活躍 HTTP／Worker、有界取消、反序關閉與 close-once。清理錯誤影響退出結果。

Scope 能力由 scope.RegistryKey 的 singleton Registry 提供；Start 之前核對所有模組 RequiredScopeKinds。缺少 Registry／Kind 或重複 Provider 皆不能 Ready。測試包含 tenant-only、organization-enabled 與替代 Provider 組裝。

`Options.StartupChecks` 在封存 Provider 後、任何 Module.Start 前依序執行。持久化應用在此註冊 Goose Schema Compatibility 與完整 Scope Inventory；失敗回收已取得資源，不能 Ready。檢查 slice 在建構時複製，nil callback 拒絕；具體 DB／組織查驗經注入 Port，Runtime 不 import Adapter。

## Root 裝配模式

`StartRoot(ctx, host, roots...)` 與 `Host` 的 `StartRoot(ctx, []any)` 方法實作見 [root.go](root.go)，進入舊路徑不變：`Host.Start(ctx, []module.Module)` 與其錯誤／清理語意原樣保留，Root 模式只透過新增進入點啟用。Root 模組是以指標提供的命名 struct instance（宣告 `ModuleMetadata`／`DependsOn` 與選用的 hook 介面），loader 在任何 hook 前完成閉環驗證。

```go
plan, err := module.LoadRoot(&appRoot{})    // 診斷用規劃，不執行任何 hook
err = runtime.StartRoot(ctx, host, &appRoot{})
```

執行順序：閉環驗證（重名、循環、無效 Type、版本不符，錯誤攜帶相依路徑）→ 三階段完整屏障（全部模組 PreConfigure → Configure → PostConfigure）→ Config 載入（模組 SetDefault 併入 defaults 層）→ provenance 封存 Seal → StartupChecks → RequiredScopeKinds → 依拓撲順序 Start → Ready。每個 hook 只取得自己的 trusted RootBuilder；hook 返回（含錯誤與 panic）後 builder 與引擎註冊把手即被撤銷，hook 外操作一律拒絕。Start／Stop 只拿到逐模組受限 Resolver，解析自身與直接相依的 export 契約，沒有全域 Resolver 逃逸。失敗與 panic 沿既有 `shutdown` 路徑：先 Drain 參與者，反序 Stop，最後 `container.Close`；建構子 Cleanup 由 Container 擁有，`Stop` 不重複關閉。

`kernel/app.Run(ctx, root, opts...)` 是簡單正式應用的預設入口：有界預算、全部 `config.StartupRules()` 必要鍵作為 defaults、opt-in `Sources`／`StartupChecks`／`Participants`，ctx 取消後以有界 Stop 收尾；不需要外部服務或環境變數。
