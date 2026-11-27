---
title: 容量、停機與恢復（維運指南）
description: 理解資料庫級有界佇列背壓（429）、Broker 離線標準恢復步驟、優雅停機順序，以及線上故障排查速查表。
---

> **你在架構中的位置**：`生產環境韌性與維運` **`[ 有界容量背壓 + 故障恢復演練 + 活躍停機 Drain ]`**
>
> **前置建議**：已完成[組裝可靠事件配送](../events/reliable-delivery.md)、[持久化背景工作](jobs.md)與[觀測與健康檢查](observability.mdx)。

## 為什麼容量控制必須做在「資料庫層」，不能只靠單機記憶體計數？

當下游 Broker 離線或消費者變慢時，如果沒有硬上限背壓，Outbox 與 Jobs 資料表會無止盡膨脹，最後把整個 PostgreSQL 磁碟塞爆，連正常查詢都一起癱瘓。若只在 API 的 Go 記憶體裡計數，多副本（例如 5 個 Pod）之間完全不同步。

panGOlin 把容量配額（`Capacity`／`MaxPending`）做成**跨程序共享的資料庫級硬上限**：

- **事件佇列**：使用 `eventpg.NewBoundedStore(pool, retry, eventpg.Capacity{Outbox: 100000, Inbox: 100000})` 並套用 `CapacityMigrations`。
- **背景工作**：在 `jobs.Policy` 明示 `MaxPending`。
- **滿額行為（Fail-closed 背壓）**：容量包含保留期內的所有終態紀錄。一旦滿額：
  - 新寫入事件或接單會直接拒絕，**該筆業務交易一起回滾**，HTTP 回傳 **`429 Too Many Requests`**（呼叫端應保留原冪等鍵並退避重試）。
  - Inbox 無法寫入新訊息時**不向 Broker 送 ACK**，讓 Broker 保留訊息待稍後重送（已存在的重複訊息仍可去重確認，同 ID 不同內容仍拒絕）。
- **規劃原則**：估算「尖峰流量 × 保留期（Retention）」配置容量與磁碟；切勿在記憶體屯積無界待送清單，也不要為了騰空間提早刪除仍在去重保證視窗內的 Inbox 紀錄。

---

## 1. Broker 離線與積壓恢復標準流程（Runbook）

當 NATS 或外部服務發生中斷再恢復時，依以下 5 步安全復原：

1. **先觀測水位**：從觀測面板查看 `pending`、`oldest`、`attempts`、`dead` 與 DB Pool 水位。只要 Outbox 還沒滿額，API 仍可正常將新業務與事件原子寫入資料庫。
2. **確認有界退避生效**：Outbox Worker 以短 `SendTimeout` 與指數退避重試；超過 `MaxAttempts` 的訊息會安全轉入 `dead`（不阻塞新訊息）。
3. **修復基礎設施**：恢復 Broker 連線、憑證、Route、FileStorage 與持久化設定；尚在 `pending` 的訊息會自動恢復派送。
4. **審查並人工重送 `dead`**：查明失敗原因（例如 Schema 不相容或目標組織授權變更）並修復後，以具備當前權限與同交易 Audit 的管理入口執行 `ReplayDead`／`Replay`，保留原 `EventID`／`JobID` 與完整 Owner。
   - **期限已過的 Job**：先對帳確認結果，再建立新操作，不可用人工重送偷偷延長過期 Deadline。
5. **獨立對帳**：使用獨立連線確認 `pending`/`processing` 排空、核對業務最終狀態；超過保留期後再以 `Purge` 清理成功終態（`dead` 先完成稽核封存）。

---

## 2. 正常停機與逾時強制收尾順序

```text
1. 撤回 /readyz（503）並停止接收新 HTTP 請求
2. 停止所有 Outbox / Inbox / Jobs Worker 認領新工作
3. Drain：等待進行中的 HTTP 請求與 Handler 自然完成
4. 若 Drain 逾時 ──▶ 傳遞取消訊號給 Handler ──▶ 等 Handler 真正退出並完成有界清理
5. 僅在 Handler 確認退出後，以仍有效的 lease token 交還未完成租約（不提前交還造成雙重執行！）
6. 反序呼叫各模組 Stop，最後關閉 DB Pool 與 Broker 連線
```

- Handler 必須確實遵守傳入的 `ctx.Done()`；絕不可把已結束的 HTTP Request context 拿去啟動背景工作。
- 若程序非正常崩潰（OOM／斷電），持有中的租約到期後會由其他 Worker 接管；因外部服務可能已先處理過，接管者必須沿用同一個 `Job ID`／`Event` 冪等鍵。

---

## 3. 線上常見異常排查速查表

| 線上症狀 | 立即檢查項目 | 正確處置（與嚴格禁忌） |
|---|---|---|
| **DB Pool 耗盡** | 檢查是否有長交易、未關閉的 `Rows`、或 Worker `Concurrency` 過高 | 縮短交易、修正 `defer rows.Close()`、調降並行度；**禁止開無界 goroutine** |
| **Scope 授權失敗** | 核對目前服務 grant、成員狀態與 Scope Provider 健康 | 修復授權來源或 grant；**禁止降級退回 tenant 或沿用舊 allow** |
| **Inbox 持續進 `dead`** | 檢查事件 Schema 版本、解碼規則與目標 Scope 權限 | 修復後以原 owner 執行 `ReplayDead`；**禁止任意把 `processed` 重設重跑** |
| **Migration 卡住** | 查詢 PostgreSQL Advisory Lock 持有者與部署日誌 | 排查卡住的部署 Session 並留存 log；**禁止另開第二套遷移工具硬跑** |
| **誤配 Tenant／Owner** | 立即停止受影響入口接單 | 透過受稽核的管理／修復路徑處理；**禁止關閉 RLS 或私下改寫資料 owner** |

> **RTO／RPO 提醒**：務必在隔離環境實際演練 Broker 故障、滿額恢復與活躍停機並記錄曲線。**RTO** 從事故發生計時到服務與對帳完全恢復；**RPO** 須由真實資料庫備份還原差異量測，不能以一次重送成功推論零資料損失。

## 下一步

- 執行升級或災難復原演練前，務必閱讀 **[備份、還原與升級檢查](backup-restore.md)**。
- 遇到特定錯誤代碼？直接查 **[疑難排解](../reference/troubleshooting.md)**。
