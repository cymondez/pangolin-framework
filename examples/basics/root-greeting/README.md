# 可設定的問候應用（Root 預設）

從 `examples/basics` 執行 `go run ./root-greeting`，輸出 `Hello, World!`。設定 APP_NAME=Reader 後輸出 `Hello, Reader!`。

這是使用者循序教學的完整程式：Module 在 Configure 以 `b.SetDefault` 提供名稱預設、以 `b.Services().Add(NewGreetingService, module.Singleton())` 註冊服務建構子，Start 以 `rt.Services().Resolve` 解析並呼叫服務，`kernel/app.Run` 以 context 期限收尾。不需要外部服務。網站直接顯示本檔程式，修改時同步教學與預期輸出。
