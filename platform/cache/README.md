# Scoped Memory Cache

`NewMemory(application, version, capacity)` 建立有界 Memory Adapter。`Get`、`Set`、`Delete`、`Load` 都從可信 context 取得完整 Tenant／Scope；Resource 只提供 module／kind／id。Private 資料另包含 principal kind／ID／authority revision。不得將 permission 決策當成永遠有效的快取。

TTL 必須大於零；容量滿時淘汰最早到期項目。值採副本；Load 合併相同 key 的並行 miss，等待者可獨立取消，loader 使用呼叫者的 deadline。Delete 會阻止先前進行中的 fill 重建舊值；失效仍需配合來源版本或 TTL。Stats 提供無 tenant label 的命中、miss、load、失效及淘汰計數。

沒有背景 goroutine 或 Close 責任。loader 的錯誤保留 cause，panic 會釋放等待者後重拋。驗證：`go test ./platform/cache`；v0.1.0 僅交付 Memory，不提供 Redis 或 DB 原子一致性。

NewMemory 可另注入一個 Publisher。Delete 先移除本機值，再發布含 application／version／完整 Scope 的 Invalidation；通知失敗保留 cause，不復活已刪除資料。Consumer 以重新授權的 scope context 呼叫 Apply，只清除該 owner 的同資源及其 principal／revision 變體，不再發布造成迴圈；其他 Scope 的事件拒絕。可靠通知與重試由注入的 Publisher／Outbox 組裝負責，Memory 自身沒有跨程序交付保證。Stats.Coalesced 記錄合併等待者。
