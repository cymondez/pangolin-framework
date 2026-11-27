# 第一個模組

從 `examples/basics` 執行 `go run ./hello`，輸出 `Hello, panGOlin!`。不需要設定或外部服務。

本例是 Root 裝配的最小應用：`helloModule` 以 `module.Base` 宣告名稱與版本、以 `Start` 方法宣告啟動工作，`kernel/app.Run` 組合並啟動根模組，context 期限結束後反序停機。`app.Run` 內建必要啟動設定，不會建立資料庫。多模組順序見相鄰 `root-lifecycle` 範例。網站直接顯示本檔程式，修改時同步教學與預期輸出。
