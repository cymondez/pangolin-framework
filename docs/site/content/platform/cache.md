---
title: Scoped Memory Cache（多租戶隔離快取）
description: 使用自動帶入完整 Tenant／Scope 鍵的記憶體快取，搭配 TTL、併發載入合併與失效通知，徹底杜絕多租戶快取串資料。
---

> **你在架構中的位置**：`Application / Platform 讀取路徑` **`[ platform/cache（自動帶 Scope Key + TTL + Load 合併 + 失效事件）]`**
>
> **前置建議**：先執行[分層設定完整範例](settings.mdx)（`go run ./platform-services`），觀察第一行輸出 `Tenant cache isolated: true`。

## 為什麼多租戶系統不能直接用普通的 `map` 或手拼快取 Key？

在多租戶 Go 應用裡，自己手拼快取 Key（例如 `fmt.Sprintf("book:%s", id)`）幾乎遲早會釀成資安事故：
- 忘了把 `tenant_id` 或 `scope_id` 拼進 Key → 客戶 B 直接讀到客戶 A 的機密資料！
- 權限相關的查詢結果忘了把 `principal` 與 `authority revision` 放進 Key → 被撤權的人繼續命中舊快取！
- 高併發下快取過期瞬間，100 個請求同時打穿進資料庫（Cache Stampede）。

`platform/cache` 從設計上直接消除這三個問題。

---

## 1. 自動隔離的快取 Key 結構

建立快取時透過 `cache.NewMemory(application, version, capacity)` 明示應用名稱、資料結構版本與最大筆數上限。之後呼叫 `Get`、`Set`、`Delete`、`Load` 只需傳入**受信任的 context** 與 `cache.Resource{Module, Kind, ID}`，框架會自動組出完整隔離鍵：

```text
一般資源鍵：application / version / tenant / scope_kind / scope_id / module / kind / id
Private=true（權限相關結果）：再加上 principal_kind / principal_id / authority_revision
```

- **為什麼 `Resource` 不讓你自己填 `TenantID`？** 因為 Tenant 與 Scope 一律從驗證過的 `context.Context` 自動取得，徹底杜絕人為漏填或填錯。
- **快取命中 ≠ 跳過權限檢查**：任何業務操作在讀取或回傳快取結果前，仍應執行當前的 Permission 檢查。

---

## 2. 併發防擊穿（`Load`）與記憶體保護

- **TTL 必須大於零**：`Set` 與 `Load` 不接受零值或負數 TTL。
- **防值拷貝汙染與容量上限**：Memory Cache 只保存位元組副本（外部修改原 slice 不會汙染快取）；達到 `capacity` 上限時自動淘汰最早到期的項目。
- **併發載入合併（Coalescing）**：使用 `Load(ctx, resource, ttl, loader)` 時，若同一時間有多個請求同時 miss 同一個 Key，框架只會呼叫一次 `loader`，其餘等待者共享結果（且每個等待者都能獨立依自己的 `ctx` 取消等待）。若 `loader` 執行失敗，不會把錯誤或空值填入快取。

---

## 3. 失效通知（Invalidation）與一致性邊界

1. **防止舊載入回填**：呼叫 `Delete` 移除本機快取時，會同時攔截「在 `Delete` 之前就已經開始跑的 `loader`」，防止慢一步回來的舊資料把剛清掉的快取又覆寫回去。
2. **跨節點失效通知**：`NewMemory` 可選注入 `Publisher` 發布含 `application`／`version`／完整 `Scope` 的失效事件。接收端須先依當前服務資格重新授權該 Scope，再呼叫 `Apply`（不同應用／版本／Scope 的事件直接拒絕；`Apply` 會一併清除該資源底下的所有 `principal`/`revision` 變體且不再重複發布）。
3. **跨程序可靠通知與 0.1.0 邊界**：
   - `platform/cache` 的 Memory Adapter 本身**沒有跨程序共用或可靠配送保證**（如需跨程序可靠失效，請搭配 [Outbox](../events/reliable-delivery.md)）；**Redis Adapter 目前（0.1.0）尚未提供**。
   - 資料庫 Commit 後的 `cache.Delete` 失敗**不代表業務交易回滾**；對一致性高度敏感的讀取請加入來源版本核對（如 [Settings](settings.mdx) 的做法）或直接查來源資料庫。

---

## 4. 觀測指標與資源清理

- 呼叫 `Stats()` 可取得 `Hits`、`Misses`、`Loads`、`Coalesced`、`Invalidations`、`Evictions` 計數（不帶無界的 Tenant／Organization label，防止 Metrics 爆炸）。
- 本服務不啟動背景常駐 goroutine，因此不需要額外 `Close()`，由建立它的組裝模組擁有生命週期。

## 下一步

接下來進入背景工作與線上維運章節：從 **[持久化背景工作（Jobs）](jobs.md)** 開始，學習如何把耗時任務與業務寫入同交易接單並可靠執行。
