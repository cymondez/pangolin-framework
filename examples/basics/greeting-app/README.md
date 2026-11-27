# 可設定的問候應用（進階相容）

從 `examples/basics` 執行 `go run ./greeting-app`，輸出 `Hello, World!`。設定 APP_NAME=Reader 後輸出 `Hello, Reader!`。

本例是進階相容路徑：明示的 `module.Module` 結構值與 `Host.Start` 清單組裝，Module 內以 `b.Provide`（`di.Provider`）註冊 GreetingService Singleton，Start 解析並呼叫服務，結束時以有界 context 停止 Host。Root 裝配的預設做法見相鄰 `root-greeting` 範例。不需要外部服務。
