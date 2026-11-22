# 第一個模組

從 `examples/basics` 執行 `go run ./hello`，輸出 `Hello, panGOlin!`。不需要設定或外部服務。

本例透過 Runtime Host 啟動 hello Module，執行完畢以有界 context 停止 Host，保留清理錯誤。四個 UOW 設定是操作逾時預設，不會建立資料庫。多模組順序見相鄰 `lifecycle` 範例。
