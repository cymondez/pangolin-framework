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
