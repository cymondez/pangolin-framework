# 應用啟停（Root 預設）

從 `examples/basics` 執行 `go run ./root-lifecycle`，依序輸出 `start storage`、`start books`、`stop books`、`stop storage`。storage 是名稱示範，並未連接真實資料庫。

本例是 Root 裝配的多模組應用：`booksModule` 以 `module.Of[storageModule]()` Type 參照宣告相依，`kernel/app.Run` 把 books 當根模組，loader 閉合相依圖（storage 自動納入、只規劃一次），全部組態完成後依 storage → books 啟動，context 期限結束後反序停止。

需要明示清單組裝（`Host.Start` 與 `host.Ready()`）時，使用相鄰的 `lifecycle` 範例的進階相容版本。
