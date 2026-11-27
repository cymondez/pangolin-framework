# 應用啟停（進階相容：明示清單組裝）

從 `examples/basics` 執行 `go run ./lifecycle`，依序輸出 `start storage`、`start books`、`ready: true`、`stop books`、`stop storage`。storage 是名稱示範，並未連接真實資料庫。

本例是進階相容路徑：以 `module.Module` 結構值明示清單交給公開 Runtime Host 執行完整組態、封存、啟動與反序停止，並以 `host.Ready()` 查詢就緒。Host 必要的四個 UOW 設定提供操作逾時預設，不會因此建立資料庫。以有界 context 停機並保留清理錯誤。Root 裝配的預設做法見相鄰 `root-lifecycle` 範例。
