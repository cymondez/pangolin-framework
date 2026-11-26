# App：根組裝應用的預設 Kernel 入口

`kernel/app` 為根組裝（root composition）應用提供一個有界預算、零外部依賴的 Kernel 入口：[Run(ctx, root, opts...)](app.go) 透過 `runtime.StartRoot`（見 [runtime 指南](../runtime/README.md)）啟動 Root 模組，等待 ctx 取消後以有界 `Stop` 收尾。

```go
err := app.Run(ctx, &myapp.Root{})
// 或：
err := app.Run(ctx, &myapp.Root{},
    app.Sources(config.Source{Name: "environment", Values: env}),
    app.StartupChecks(checkSchema),
    app.Participants(httpDrain, workerDrain),
)
```

- 內建 defaults 涵蓋 [config.StartupRules](../config/README.md) 全部必要鍵（uow.default_timeout、uow.cleanup_timeout、uow.hook_timeout、uow.max_requires_new_depth），正式應用不需要環境或外部服務即可啟動。
- `Sources` 的每一個 caller "defaults" source 同層覆蓋內建 defaults（後者在前者之下）。值一律複製，不改寫呼叫端 map；非法值在 `config` 驗證失敗，任何 hook 都不執行。file／environment／cli 層保持既有更高優先序。
- Runtime 的 drain／cleanup 預算目前是固定常數（10s／5s），不透過組態覆蓋；這不是組態契約，需要調整時以明示 Runtime 選項擴充，不會以組態值暗示可設定。
- `Stop` 預算 25s；ctx 已取消後仍以 `WithoutCancel` 執行收尾。
- `Roots` 可提供需配置的相依模組實例。Root loader 仍會檢查完整 Type 相依圖。

生命週期、失敗與證據責任同 [runtime](../runtime/README.md)：任何啟動錯誤不報 Ready、反序 Stop、Container 單一擁有關閉責任。驗證：`go test ./kernel/app`。
