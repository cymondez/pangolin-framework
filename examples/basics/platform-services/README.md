# Cache、Settings 與 Features

從 examples/basics 執行 `go run ./platform-services`；不需要外部服務。程式示範完整 Tenant key、設定覆寫、quota 父層上限、Permission 優先及通知失敗後的版本收斂。console identity 是明示的教學 fixture；HTTP 應用使用正式身分／Tenant／Scope pipeline。

預期七行：Tenant cache isolated: true、Tenant setting: "blue"、JSON array setting: "mail"、Effective quota: 10、Feature cannot grant permission: true、Stored update despite notification failure: true、Version converged: true。所有 Memory 服務都由 main 擁有，沒有背景資源需清理。
