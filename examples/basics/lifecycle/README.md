# 應用啟停

從 `examples/basics` 執行 `go run ./lifecycle`，依序輸出 `start storage`、`start books`、`ready: true`、`stop books`、`stop storage`。storage 是名稱示範，並未連接真實資料庫。

本例使用公開 Runtime Host 執行完整組態、封存、啟動與反序停止。Host 必要的四個 UOW 設定提供操作逾時預設，不會因此建立資料庫。以有界 context 停機並保留清理錯誤。
