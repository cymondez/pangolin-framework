# Module：註冊與相依規劃

`Module` 是編譯期明確註冊的功能單位。此套件只驗證及規劃模組圖；實際生命週期由 [runtime](../runtime/README.md) 執行。公開契約見 [contracts.go](contracts.go)，實作見 [registry.go](registry.go)。

```go
registry := module.NewRegistry()
err := registry.Register(module.Module{
    Name: "books", Version: "0.1.0",
    DependsOn: []module.Dependency{{Name: "database", VersionConstraint: ">=0.1.0 <0.2.0"}},
})
// 必須也註冊 database；檢查每次 Register 的 err，再呼叫 Plan。
plan, err := registry.Plan()
```

Name 不可空白或重複。Version 採 SemVer；依賴只接受 `=0.1.0` 或 `>=0.1.0 <0.2.0`，預發行只接受精確相等；build metadata 不影響版本比較。不接受 wildcard／caret／tilde／OR。缺漏、版本不符及循環會回傳包含相依路徑的錯誤。

Plan 採逐層 Kahn 排序，每層依名稱排序。Register 複製切片，Plan 回傳 detached snapshot；呼叫端修改切片不會改寫 Registry。Runtime 必須在任何 configure／Start 前取得合法 Plan。

各模組的 Start 失敗時，該模組負責收回尚未交付的部分資源；已成功啟動的模組由 Runtime 反序 Stop。若資源由 DI Factory 的 Cleanup 所擁有，Stop 不得再關閉同一資源。完整階段順序與停機方式見 Runtime 說明。

驗證：`go test ./kernel/module ./kernel/runtime`。A01 涵蓋非法圖、版本約束及穩定排序；A02 驗證生命週期與失敗清理。M1 不提供熱插拔。
