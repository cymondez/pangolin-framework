# Cache、Settings 與 Features

從 examples/basics 執行 `go run ./platform-services`；不需要外部服務。程式示範完整 Tenant key、設定覆寫、quota 父層上限、Permission 優先及通知失敗後的版本收斂。console identity 是明示的教學 fixture；HTTP 應用使用正式身分／Tenant／Scope pipeline。

預期八行：

```text
Tenant cache isolated: true
Tenant setting: "blue"
JSON array setting: "mail"
Setting layers: [user tenant global config default]
Effective quota: 10
Feature cannot grant permission: true
Stored update despite notification failure: true
Version converged: true
```

Settings 依目前可信 Scope 由高至低解析；tenant-only 的五層為 User、Tenant、Global、Config、Default。Global 是可更新的全域來源，Config 是獨立的唯讀組態來源。啟用組織時，加入可信 Enterprise／Organization 層。Feature 評估先檢查 Permission，功能值不能授予權限。所有 Memory 服務都由 main 擁有，沒有背景資源需清理。
