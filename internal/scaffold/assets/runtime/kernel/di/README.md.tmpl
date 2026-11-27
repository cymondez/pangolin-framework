# DI：明確 Provider 與資源生命週期

此套件依 constructor／Factory 明確組裝服務，不掃描物件、不使用全域 service locator。契約見 [contracts.go](contracts.go)，實作見 [container.go](container.go)。

```go
registry, err := di.NewRegistry(2 * time.Second)
key := di.NewKey[*http.Client]("http-client")
err = registry.Provide(di.Provider{
    Key: key, Lifetime: di.Singleton,
    Factory: func(ctx context.Context, deps di.Resolver) (any, di.Cleanup, error) {
        return &http.Client{Timeout: time.Second}, nil, nil
    },
})
// 檢查上述每個 err。
container, err := registry.Seal(ctx)
// Seal 成功後由擁有者在結束時 container.Close(ctx)。
```

Token 身分由 Name 與 Go Type 共同決定。解析具名鍵 `di.Key[T]` 時可使用泛型 `di.ResolveKey[T](ctx, resolver, key)`，自動驗證 `ctx`、`resolver`、鍵名與型別並保留底層錯誤 cause。Factory 必須在 DependsOn 宣告全部可解析的鍵；收到的 Resolver 只在 Factory 執行期間有效。重複、缺漏、循環與 Singleton 經 Transient 捕獲 Operation 的圖，在 Seal 前拒絕。`Replace(provider, reason)` 要求已存在的鍵與非空原因；Registry 保存替換原因，Seal 後禁止 Provide／Replace。

- Singleton 在 Seal 時建立，可安全共用；Container 最後反序清理。
- Transient 每次解析建立；從 Operation Scope 解析時由該 Scope 擁有 Cleanup，其餘由 Container 擁有。
- Operation 由 `OpenScope(ctx)` 建立，Scope 內同鍵只建立一次；呼叫端必須 `Close`，結束後不能再 Resolve。Container 會回收尚未關閉的 Scope。

Factory 回傳 error 時仍會執行其回傳的 Cleanup，保留主要與清理錯誤；Singleton 建構失敗／panic 會回收已取得資源。Cleanup 必須遵守 context deadline，避免阻塞停機；同一資源只能指定一個 owner。不要在 Module.Stop 再次關閉 Factory 擁有的連線池。

Factory 與 Cleanup 都在 container metadata lock 之外執行。Singleton 使用 provider key 協調單次建構；Operation 使用 scope 與 provider key 協調單次建構，同 key 的等待者可由自己的 context 取消。其他 key 的 Resolve 與 OpenScope 不受慢 Factory 或 Cleanup 阻塞。Factory Resolver 在 Factory 返回時立即失效，且只可解析 DependsOn 中宣告的服務。

Scope／Container Close 先停止新操作並取消進行中的 Factory context，再等已進入的解析離開後清理資源。呼叫端提供的 deadline 或 Registry cleanup timeout（取較早者）限制 Close 等待時間；僅取消訊號不會略過清理。若 Factory 忽略取消而晚回傳資源，背景收尾仍會接手並清理，Close 不會把該資源交給呼叫者或漏掉清理。並行與重複 Close 共用同一次清理結果；Container 會等所有 Operation Cleanup 完成後才清 Singleton。每組資源依反向建構順序清理。

驗證：`go test -count 1 -timeout 120s ./kernel/di`。A46 包含三種 lifetime、明確替換、封存、相依錯誤、部分失敗、Scope 清理、單一建構及慢 Factory／Cleanup 下互不相關的併發解析。
