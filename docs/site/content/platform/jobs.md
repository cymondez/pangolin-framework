---
title: 持久化背景工作
description: 將工作和業務資料一起保存，以目前服務資格執行，管理重試、取消與重送。
---

當回覆 API 後仍需寄收據、產生報表或呼叫外部服務時，可以保存一筆 Job，交給獨立 Worker 執行。Job 具有穩定 ID、完整 owner、actor、payload 版本、執行時間與期限；它採至少一次執行，Handler 必須能安全處理重複。

本頁適用已完成[PostgreSQL 組裝](../data/postgresql.md)及[身分與 Scope](../security/resource-scopes.md)的應用。只想先認識版本化資料，可從[事件範例](../events/integration-events.mdx)開始。

## 1. 部署工作資料表

部署程序將 `jobs/postgres.Migrations` 的 `migrations` 子目錄與 Tenant、Scope Catalog、Audit 來源合併，jobs 宣告依賴 scope。使用[資料庫遷移](../data/migrations.md)的單一 Goose Runner；應用啟動只檢查相容性。app／worker 連線不得是 table owner 或 superuser。

## 2. 組裝收據 Worker

在自己的 Go module 新增 `setup/jobs.go`，貼入以下完整檔案。你提供的 sendReceipt 必須使用傳入的 idempotencyKey，呼叫支援冪等的外部服務。

```go
package setup

import (
    "bytes"
    "context"
    "encoding/json"
    "github.com/cymondez/pangolin-framework/platform/jobs"
    jobpg "github.com/cymondez/pangolin-framework/platform/jobs/postgres"
    "github.com/jackc/pgx/v5/pgxpool"
)

type Receipt struct {
    ReceiptID string `json:"receipt_id"`
}

func ReceiptWorker(queuePool *pgxpool.Pool, config jobs.Policy,
    bind jobs.Bind, checkActor jobs.CheckActor,
    authorize func(context.Context, string) error,
    sendReceipt func(context.Context, string, string) error,
) (*jobpg.Store, *jobs.Worker, error) {
    if sendReceipt == nil { return nil, nil, jobs.ErrInvalid }
    decode := func(raw json.RawMessage) (Receipt, error) {
        var receipt Receipt
        decoder := json.NewDecoder(bytes.NewReader(raw))
        decoder.DisallowUnknownFields()
        if decoder.Decode(&receipt) != nil || receipt.ReceiptID == "" {
            return receipt, jobs.ErrInvalid
        }
        return receipt, nil
    }
    registry, err := jobs.NewRegistry(64*1024, jobs.Definition{
        Type: "receipt.send", Version: 1,
        Validate: func(raw json.RawMessage) error { _, err := decode(raw); return err },
        Handle: func(ctx context.Context, job jobs.Job) error {
            receipt, err := decode(job.Payload)
            if err != nil { return err }
            return sendReceipt(ctx, receipt.ReceiptID, job.ID)
        },
    })
    if err != nil { return nil, nil, err }
    store, err := jobpg.NewStore(queuePool, registry, config, authorize)
    if err != nil { return nil, nil, err }
    worker, err := jobs.NewWorker(store, registry, config, "receipts", bind, checkActor, nil)
    if err != nil { return nil, nil, err }
    return store, worker, nil
}
```

在自己的專案目錄執行 `go mod tidy`、`go build ./...`，預期沒有錯誤。這段組裝不會自行連線、執行遷移或啟動工作；pool 由你的資料庫 Module 建立及關閉。

bind 每次必須從可信服務憑證建立新 context，依目前 Tenant 資格與 Job.Owner 重新 Bind，再檢查 `jobs.execute` Permission。Worker 也會比對 binding 的完整 owner 與 service Principal。組織工作需要目前有效的組織服務 grant，沒有 Provider 或無法授權就進有界重試，不能退回 tenant scope。詳見[身分驗證](../security/authentication.md)與[授權](../security/authorization.mdx)。

authorize 用來檢查 `jobs.enqueue`、`jobs.cancel`、`jobs.replay`。代理使用者的 Job 設 `Delegated: true`，checkActor 另驗該使用者目前資格；一般工作保存 Actor 作為歸屬記錄，執行授權來自目前服務身分。

## 3. 設定上限

Policy 的 Lease、ExecutionTimeout、CleanupTimeout、PollInterval、RetryInitial、RetryMax、Retention、MaxAttempts、Concurrency、BatchSize、MaxPending 都需明示；JitterFraction 為 0–1。可從下表開始，再依自己的負載調整：

| 項目 | 起始值 |
|---|---|
| Lease／執行／清理期限 | 30s／20s／5s |
| PollInterval | 100ms |
| Concurrency／BatchSize | 4／32 |
| MaxAttempts／退避／jitter | 8／250ms–30s／0.2 |
| MaxPending／Retention | 100000／720h |

Lease 必須大於執行加清理期限。首版一次認領一筆，永遠不超過 BatchSize；沒有先把整個佇列讀進記憶體。MaxPending 包含保留中的全部紀錄，滿額回 `JOB_CAPACITY_EXCEEDED`，HTTP 映射 429；呼叫端保留同一操作識別並退避。

## 4. 與業務一起接單

在已授權的可寫 UOW callback 內，建立 Job 並呼叫 `store.Enqueue(work, job)`。Job.Owner 和 Job.Actor 必須等於 `scope.Current(work)` 的 Ref／Principal；Payload 只能放業務參數，不能提供權限。DueAt 是最早執行時間，Deadline 必須晚於 DueAt。

工作與業務資料共用 Commit／Rollback；提交前崩潰不會留下可執行工作。Job ID 在首次接單前產生，重試／人工重送保持不變。需要防止客戶端重複接單時，使用[Command 冪等鍵](../data/idempotency.md)。

需要追蹤時，在接單處將 `observability.OperationID(work)` 與 `observability.TraceParent(work)` 分別存入 Job.OperationID／TraceParent；這些值只用於關聯，不作授權。

## 5. 啟動與停止

在 Worker 程序的 Module.Start 呼叫 `worker.Start()`，並把 worker 傳入 `runtime.New(..., worker)` 的 Participant 清單。API、排程接單與 Worker 可以是不同程序，共用持久化 queue。Handler 的資料庫工作使用明示 Command／Query UOW，工作完成狀態與業務交易分開，不宣稱跨程序原子提交。

停機先撤回 Ready、停止新接單和認領，再 Drain。逾時取消 Handler，等它退出並完成有界清理後，才用有效 token 交還 lease，最後關閉 DB。不要用已結束的 HTTP context 啟動 Worker。

## 6. 取消、失敗與重送

在目前 owner 的授權可寫 UOW 呼叫 `store.Cancel(ctx, id, trace)` 或 `store.Replay(ctx, id, trace)`；必要 Audit 與狀態更新同交易提交。Replay 只接受仍在 Deadline 內的 dead 工作，保留原 ID／owner／schema。期限已過時先對帳，再建立新操作，不能偷偷延長舊期限。

只有成功／取消終態超過 Retention 才可由 `Purge` 清除；dead 保留供修復與稽核。若外部服務成功後程序崩潰，Handler 可能再次被呼叫，外部服務應以同一 Job ID 回傳原結果。

下一步：[觀測與健康檢查](observability.mdx)，查看失敗與積壓；[容量與恢復](operations.md)說明滿額和 Broker 離線時的處置。
