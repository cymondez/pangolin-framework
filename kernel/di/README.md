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

Token 身分由 Name 與 Go Type 共同決定。Factory 必須在 DependsOn 宣告全部可解析的鍵；收到的 Resolver 只在 Factory 執行期間有效。重複、缺漏、循環與 Singleton 經 Transient 捕獲 Operation 的圖，在 Seal 前拒絕。`Replace(provider, reason)` 要求已存在的鍵與非空原因；Registry 保存替換原因，Seal 後禁止 Provide／Replace。

- Singleton 在 Seal 時建立，可安全共用；Container 最後反序清理。
- Transient 每次解析建立；從 Operation Scope 解析時由該 Scope 擁有 Cleanup，其餘由 Container 擁有。
- Operation 由 `OpenScope(ctx)` 建立，Scope 內同鍵只建立一次；呼叫端必須 `Close`，結束後不能再 Resolve。Container 會回收尚未關閉的 Scope。

Factory 回傳 error 時仍會執行其回傳的 Cleanup，保留主要與清理錯誤；Singleton 建構失敗／panic 會回收已取得資源。Cleanup 必須遵守 context deadline，避免阻塞停機；同一資源只能指定一個 owner。不要在 Module.Stop 再次關閉 Factory 擁有的連線池。

驗證：`go test ./kernel/di`。A46 包含三種 lifetime、明確替換、封存、相依錯誤、部分失敗、Scope 清理與併發解析。
