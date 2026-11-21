# ADR06 — 錯誤與冪等

狀態：採用於 M0；實作待 M2／M3。對應 ENV06、API-001–002、IDEM-001–003、SCP-009。

採規範 §3.3、§5.6 的 Problem Details／sentinel／cause／Outcome。Commit Unknown 不等同 RolledBack，不自動重跑 callback；回應完全遺失需使用者在送出前已知的 Idempotency-Key。

冪等鍵包含完整 tenant／scope／principal／operation／key，同交易保留與保存成功結果。唯一約束協調併發，禁止先查後寫取代約束。結果重播重新授權，撤權不刪防重紀錄。local profile 固定 key 上限 128 bytes、body 1 MiB、公開安全重試窗口 24h、結果保留 7d；查無紀錄仍不能推斷 Unknown 已失敗。

Broker 保留 7d、人工重送窗口 14d、Inbox 防重保留 30d；Outbox sent 保留 30d、Audit 起始保留 90d。這些是待容量驗證的本機基線，調整必須維持防重／重播窗口契約。重試最多 8 次、250ms 起始／30s 上限、有 jitter。數值不能改變已提交 Outcome。
