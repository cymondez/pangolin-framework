# Root 裝配正式示範應用

`moduleapp` 是 Root 裝配模式（Root module loading ＋ constructor DI）的可執行正式示範，獨立 Go module 透過本機 replace 引用 Framework。它只使用 kernel 公開 API：

- 根模組以 Type 參照聚合 catalog 與 pricing 兩個相依（`app/module.go` 的 `DependsOn() []module.Ref`）。
- store 是 catalog 與 pricing 共同的菱形相依，loader 依 Type 去重、只規劃與建構一次（`store/module.go` 以 `module.AddSingleton` 註冊並 Export 自身具象服務）。
- catalog 是 `catalog/contracts` 公開介面契約的擁有者：註冊實作、Export 契約；消費方（app root）只相依 catalog 模組，再以 `module.Resolve[T]` 透過受限 Resolver解析該契約。
- pricing 以 `module.AddTransient` 註冊，示範 Transient 生命週期與精確具象鍵解析；未 Export 的服務對其他模組不可見。
- main 以 `module.LoadRoot` 輸出規劃診斷（不執行任何 hook），再以 `kernel/app.Run` 啟動：完整三階段屏障、provenance 封存、依拓撲順序 Start、ctx 取消後反序 Stop。app Start 透過 `rt.OpenScope(ctx)` 建立固定 owner 的操作 scope，再以 `module.Resolve[T]` 解析契約。

從本目錄執行 `go run .`。預期看到規劃層、`start store` 與 catalog/pricing/app 的 Start 診斷；Ctrl+C 取消後看到反序 Stop（app → pricing/catalog → store）。非互動驗證可設 `MODULEAPP_DEADLINE=2s` 讓示範自行依時限退出（訊號處理仍是預設行為）。生命週期細節見 `docs/guides/root-module-di.md` 與各子目錄 README。
