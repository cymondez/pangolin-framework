---
title: HTTP 入口與稽核
description: 組裝驗證、Scope、授權、解碼、短交易與安全記錄。
---

先完成[授權範例](authorization.mdx)，並有可用的 UOW Manager。`transport/http.NewPipeline` 接受明確的函式相依，適合你自己的 HTTP Handler。

## 入口順序與設定

建立 `PipelineOptions` 時提供 `Timeout`、`MaxBodyBytes`、`Manager`、`Authenticate`、`ResolveTenant`、`ResolveScope`、`Entry`、`Decode`、`Work`；缺少必要設定會回錯誤。`ReadOnly` 選擇 Query，預設使用 Command；可另傳 `ResourcePolicy` 與 `Rejected`。

| 階段 | 你的函式負責什麼 |
|---|---|
| Authenticate | 用可信 issuer／JWKS 驗證 Bearer Token，回傳新的 context |
| ResolveTenant／ResolveScope | 依目前資格驗證候選 selector，安裝可信 binding |
| Entry | 檢查 action Permission；拒絕時不解碼業務 DTO |
| Decode | 在 body 上限內解碼並驗證 DTO |
| UOW／ResourcePolicy／Work | 用 callback context 讀取資源、檢查資源規則並執行 Use Case |
| 完成與回應 | Commit 確認成功後序列化；失敗回 Problem Details |

這是單次非串流入口。SSE／WebSocket 請使用每次操作的短 Scope，見[工作單元](../data/unit-of-work.md)。敏感 Use Case 自身仍需檢查權限，避免由其他入口直接呼叫時漏驗。

DTO 不決定 owner。路由與 Header 是候選值；使用 `scope.Select` 拒絕衝突，資源 DTO 若帶 owner，使用 `scope.MatchOwner` 對照可信 binding。不要把自行建立的 ScopeBinding 當作認證。

## 兩種記錄

`platform/audit.Recorder.Record` 寫入目前可寫 UOW 的 PostgreSQL Executor。必要稽核與業務／Outbox 同交易；必須將記錄錯誤傳回 callback，讓交易失敗。部署前安裝 Audit migration。

`audit.Record` 只接受 Action、Resource、Result、Trace 與非負 Duration；識別欄位使用 1–160 個字母、數字或 `_ . : -`。Actor 與完整 owner 從可信 context 取得，不接受任意 request body、Token、SQL 或原始錯誤內容。

登入／Scope／Permission 拒絕使用獨立的 `audit.NewSecurityLog(writer, authn.Identity)`，接到 Pipeline 的 `Rejected`。它不依附業務交易，因此回滾後仍可留下記錄；尚未綁定 Scope 時只保存已驗證 Actor，不記錄未授權的 owner hint。部署的 writer 需自行設定有界寫入及清理，並妥善處理記錄錯誤。

## 提交之後出錯

Problem Details 會遮罩內部細節。提交後序列化或傳輸失敗，資料仍可能已提交，回應保留操作識別與 Committed outcome。收到 `UOW_COMMIT_UNKNOWN` 時保留原冪等 key，依[結果對帳](../data/idempotency.md)確認；不要直接換新 key 重送。

Host 跨租戶查詢使用 `policy.Host.Run` 的獨立控制面：明示目標 Tenant／action、Host Policy 與必要 Audit。不要為一般 Repository 建立空 Tenant 或萬用 Scope。
