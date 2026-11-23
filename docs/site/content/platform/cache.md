---
title: Scoped Memory Cache
description: 用完整 Tenant／Scope key、TTL 與載入合併快取應用資料。
---

先執行[分層設定完整範例](settings.mdx)。第一行 `Tenant cache isolated: true` 表示相同 resource ID 在另一個 Tenant 查不到目前值。

## 建立與使用

使用 cache.NewMemory(application, version, capacity) 明示應用名稱、資料版本與最大筆數。Get、Set、Delete、Load 都接受目前可信 context 及 Resource；Resource 只提供 Module、Kind、ID。

實際 key 包含 application／version／tenant／scope_kind／scope_id／module／resource／id。Private 為 true 時，再加入 Principal kind／ID 與 authority revision，適用於結果因使用者資格而不同的資料。快取命中不代替當前 Permission 檢查。

Set 與 Load 的 TTL 必須大於零。Memory 只保存值的副本；容量滿時淘汰最早到期項目。Load 接受自己的 loader，合併同一 key 的並行 miss；等待者可獨立取消。loader 的期限由呼叫 context 控制，失敗不填入快取。

## 失效與通知

Delete 會移除目前值，並阻止之前開始的 loader 再填回舊值。NewMemory 可注入一個 Publisher，發布包含 application／version／完整 Scope 的 Invalidation。通知失敗回傳原 cause，已移除的本機值不會復活。

Consumer 先依目前服務資格重新授權該 Scope，再呼叫 Apply；其他 application、version 或 Scope 的事件會被拒絕。Apply 清除同資源的 principal／revision 變體，且不重複發布。跨程序可靠通知可接 Outbox；Memory 本身沒有可靠配送或跨程序共用的保證。

對資料一致性敏感的讀取，保留來源版本核對或直接讀取來源。Commit 後的 cache delete 也可能失敗，不能據此推論業務資料回滾。Settings 已提供來源版本核對；一般 Repository 查詢需由應用選擇適合的策略。

## 計數與清理

Stats 提供 Hits、Misses、Loads、Coalesced、Invalidations、Evictions，沒有無界 Tenant／Organization labels。服務沒有背景 goroutine 或 Close；組裝擁有它，TTL 不建立常駐清理程序。

0.1.0 提供 Memory Adapter；Redis Adapter 尚未提供。接著閱讀 [Features](features.md)，在快取及產品設定之前保留權限檢查。
