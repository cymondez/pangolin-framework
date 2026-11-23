---
title: 容量、停機與恢復
description: 設定有界佇列與重試，在 Broker 離線、滿額或停機後安全恢復。
---

本頁適用已組裝[可靠事件](../events/reliable-delivery.md)或[背景工作](jobs.md)的應用。先為認領批量、並行數、payload、lease、timeout、重試、retention 與總容量設定明確上限，再觀察自己的實際負載。

## 接單與背壓

事件使用 `eventpg.NewBoundedStore(pool, retry, eventpg.Capacity{Outbox: 100000, Inbox: 100000})`，並在部署時套用 `CapacityMigrations`。Jobs 使用 Policy.MaxPending。容量由資料庫在跨程序的 transaction lock 下檢查，不能只在單一 API 的記憶體計數。

容量包含保留中的終態紀錄。滿額時拒絕接收新工作，業務交易一起失敗；HTTP 回 429，呼叫端保留同一冪等鍵並退避。Inbox 無法持久化新訊息時不 ACK，來源仍須保留訊息；已存在的相同重複可以確認，衝突內容仍會拒絕。

不要在記憶體累積無界待送清單，或提早刪除仍在重送／去重保證窗口內的證據。先估計流量乘上保留期，再配置磁碟與容量；發生積壓時降低接單量及並行，避免同時耗盡 DB pool。

## Broker 離線後恢復

1. 查看 pending、oldest、attempts、dead 與 Pool 使用量。容量內的 API 可以將事件提交至 Outbox。
2. Worker 使用短 SendTimeout 與有界退避；達 MaxAttempts 的訊息保留在 dead。
3. 恢復 Broker 的憑證、route、FileStorage 與持久化確認，讓 pending 自動繼續。
4. 對 dead 檢查失敗原因、目前 owner grant 及 schema。以目前授權與同交易 Audit 人工 replay，保留原 Event ID／Job ID 和完整 owner。
5. 從獨立連線核對成功數與業務結果，確認 pending／processing 排空。外部副作用仍以穩定冪等鍵確認。

工作的 Deadline 已過時，先對帳再建立新操作；不要用人工重送繞過期限或授權。保留期過後才使用 Purge 清除成功終態，dead 先稽核與封存。

## 正常與逾時停機

先撤回 Readiness 並停止新請求，再停止 Worker 認領；等待活躍 HTTP 與 Handler 完成。Drain 到期時傳遞取消，等待有界 cleanup。Handler 尚未退出時不提前交還 lease；退出後只用仍有效的 token 更新或交還，最後反序關閉模組與 DB／Broker。

Handler 必須遵守 context；不要把 Request context 留給背景程序。程序崩潰時，尚未完成的工作由 lease 到期後接管；外部服務可能已成功，因此接管者仍需使用同一 Job ID／Event 冪等鍵。

## 常見狀況

| 狀況 | 檢查及處理 |
|---|---|
| Pool 滿了 | 查長交易與未關閉 Rows，降低 Worker 並行；不要建立無界 goroutine |
| Scope 授權失敗 | 核對目前服務 grant、membership 與 Provider；不退回 tenant 或重用舊 allow |
| Inbox dead | 修復 schema／授權後，以原 owner replay；不要任意重設 processed |
| Migration 卡住 | 檢查部署鎖及仍在執行的 session，保留 logs；不另開第二套 migration 工具 |
| Tenant 設錯 | 停止受影響接單，透過受稽核管理路徑修復；不關閉 RLS 或改寫工作 owner |

在自己的隔離環境演練 Broker 故障、積壓恢復和活躍停機，保存 lag／pool／retry／drain 曲線。RTO 從故障到完成對帳量測；RPO 需以 DB／schema／可靠訊息的實際備份還原結果決定，不能從一次成功重送推論零資料損失。

下一步：[疑難排解](../reference/troubleshooting.md)。
