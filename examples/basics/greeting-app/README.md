# 可設定的問候應用

從 `examples/basics` 執行 `go run ./greeting-app`，輸出 `Hello, World!`。設定 APP_NAME=Reader 後輸出 `Hello, Reader!`。

這是使用者循序教學的完整程式：Config 提供名稱、Module 註冊 GreetingService Singleton，Start 解析並呼叫服務，結束時停止 Host。不需要外部服務。網站直接顯示本檔程式，修改時同步教學與預期輸出。
