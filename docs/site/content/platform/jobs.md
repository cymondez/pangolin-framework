---
title: 持久化背景工作（Jobs）
description: 學習如何將延後或耗時任務與業務資料同交易寫入 PostgreSQL 佇列，以獨立服務資格與租約 token 可靠執行，並安全管理重試、取消與重送。
---

> **你在架構中的位置**：`API 寫入交易內（store.Enqueue 與業務同交易 Commit）→` **`[ PostgreSQL Jobs Queue → 獨立 jobs.Worker（短租約認領 + 重驗服務權限 + 有界重試）]`**
>
> **前置建議**：先完成 [PostgreSQL 組裝](../data/postgresql.md)與[資源 Scope](../security/resource-scopes.md)。

## 為什麼不能直接 `go sendReceipt()` 就好？

在 Go 裡開一條 goroutine 太容易了，導致很多新手把寄收據、產報表、打外部 Webhook 直接寫成 `go func() { ... }()`。這在生產環境有四個致命傷：
1. **程序重啟即蒸發**：Pod 滾動更新或 OOM 重啟，記憶體裡的 goroutine 全部憑空消失，沒有任何紀錄。
2. **背壓失控**：瞬間湧入 10 萬筆請求就開出 10 萬條 goroutine，直接把外部服務與自家 DB 打死。
3. **重用已結束的 HTTP Context**：HTTP 回傳 200 的瞬間 `r.Context()` 就被取消了，背景 goroutine 一跑就失敗。
4. **業務回滾卻照跑**：資料庫交易後來 Rollback 了，開出去的 goroutine 卻已經把收據寄給客戶！

panGOlin 的 `platform/jobs` 用「**與業務同交易持久化接單 + 租約制 Worker + 獨立重授權**」根治這四個問題（採 **at-least-once 至少一次執行**，因此外部呼叫必須使用 `job.ID` 作為穩定冪等鍵）。

---

## 1. 部署工作資料表

部署程序將 `platform/jobs/postgres.Migrations` 的 `migrations` 子目錄與 Tenant、Scope Catalog、Audit 一起交給[資料庫遷移](../data/migrations.md)的單一 Goose Runner，並宣告 `jobs` 依賴 `scope`。API 與 Worker 連線帳號皆不可是 table owner 或 superuser。

---

## 2. 完整組裝範例：`setup/jobs.go`

以下完整檔案可放進你自己專案的 `setup/jobs.go`（`go build ./...` 即可驗證）。傳入的 `sendReceipt` 必須把 `job.ID` 當成 `idempotencyKey` 傳給支援冪等的外部服務：

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

// Receipt 是背景工作 payload 的版本化資料結構（只放業務參數，不放憑證或權限）。
type Receipt struct {
	ReceiptID string `json:"receipt_id"`
}

// ReceiptWorker 組裝持久化 Job Store 與背景 Worker。
// 注意：本函式只做組裝，不自行連線、不跑 Migration、也不啟動 Worker；
// queuePool 由所屬資料庫模組擁有並於停機 Drain 後關閉。
func ReceiptWorker(
	queuePool *pgxpool.Pool,
	config jobs.Policy,
	bind jobs.Bind,
	checkActor jobs.CheckActor,
	authorize func(context.Context, string) error,
	sendReceipt func(context.Context, string, string) error,
) (*jobpg.Store, *jobs.Worker, error) {
	if sendReceipt == nil {
		return nil, nil, jobs.ErrInvalid
	}

	// 步驟 1：定義嚴格的 Payload 解碼器（拒絕未知欄位與空值），同時用於接單驗證與執行解碼。
	decode := func(raw json.RawMessage) (Receipt, error) {
		var receipt Receipt
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&receipt) != nil || receipt.ReceiptID == "" {
			return receipt, jobs.ErrInvalid
		}
		return receipt, nil
	}

	// 步驟 2：建立版本化 Job Registry（設定單筆 payload 上限為 64 KiB）。
	registry, err := jobs.NewRegistry(64*1024, jobs.Definition{
		Type:     "receipt.send",
		Version:  1,
		Validate: func(raw json.RawMessage) error { _, err := decode(raw); return err },
		Handle: func(ctx context.Context, job jobs.Job) error {
			receipt, err := decode(job.Payload)
			if err != nil {
				return err
			}
			// 將穩定的 job.ID 作為外部冪等鍵，避免至少一次執行（at-least-once）造成重複寄信。
			return sendReceipt(ctx, receipt.ReceiptID, job.ID)
		},
	})
	if err != nil {
		return nil, nil, err
	}

	// 步驟 3：建立 PostgreSQL Job Store 與 Worker。
	store, err := jobpg.NewStore(queuePool, registry, config, authorize)
	if err != nil {
		return nil, nil, err
	}
	worker, err := jobs.NewWorker(store, registry, config, "receipts", bind, checkActor, nil)
	if err != nil {
		return nil, nil, err
	}
	return store, worker, nil
}
```

### 兩個必懂的安全回呼：`bind` 與 `authorize`

- **`bind`（執行端重授權）**：每次 Worker 認領一筆 Job 時，必須從**可信服務憑證**建立全新 context、依目前 Tenant／組織資格重新 `Bind(job.Owner)` 並檢查 `jobs.execute` 權限。若是組織層級 Job，必須有當前有效的組織服務 grant（不可退回 tenant scope）。
- **`authorize` 與 `checkActor`**：`authorize` 檢查 `jobs.enqueue`、`jobs.cancel`、`jobs.replay`；若某筆 Job 代表使用者代理執行（`Delegated: true`），`checkActor` 會額外核對該使用者目前是否仍保有資格（防堵已離職使用者的排程繼續跑）。

---

## 3. 設定有界上限（`jobs.Policy` 建議起始值）

panGOlin 拒絕無界佇列，所有參數都必須明示正值（`JitterFraction` 為 `0–1`）。可從以下起始值開始，再依實測負載調整：

| 參數類別 | 建議起始值 | 設計因果與限制 |
|---|---|---|
| `Lease`／`ExecutionTimeout`／`CleanupTimeout` | `30s`／`20s`／`5s` | **`Lease` 必須嚴格大於 `ExecutionTimeout + CleanupTimeout`**，否則 Handler 還沒做完清理租約就到期被別人接手！ |
| `PollInterval` | `100ms` | 無工作時的輪詢間隔 |
| `Concurrency`／`BatchSize` | `4`／`32` | 嚴格控制並行度，絕不一次把整張表載入記憶體 |
| `MaxAttempts`／`RetryInitial`–`RetryMax`／`JitterFraction` | `8`／`250ms`–`30s`／`0.2` | 指數退避加隨機抖動，避免雪崩重試 |
| `MaxPending`／`Retention` | `100000`／`720h` | `MaxPending` 包含保留中的全部紀錄；滿額直接回 `JOB_CAPACITY_EXCEEDED`（HTTP 429），保護資料庫 |

---

## 4. 在業務交易中接單、啟動與安全收尾

### 接單（與業務同交易）
在已授權的可寫 UOW 回呼內呼叫 `store.Enqueue(work, job)`：
- `job.Owner` 與 `job.Actor` 必須等於 `scope.Current(work)` 的 `Ref` 與 `Principal`；`DueAt` 為最早執行時間，且 `Deadline` 必須晚於 `DueAt`。
- 業務寫入與 Job 寫入**同交易 Commit／Rollback**；若交易失敗，絕不會留下幽靈 Job。
- 想把 API 請求與背景執行連成同一條分散式追蹤？接單時順手把 `observability.OperationID(work)` 與 `observability.TraceParent(work)` 填入 `job.OperationID`／`job.TraceParent`（僅供追蹤關聯，不作為授權依據）。

### 啟動與優雅停機
- 在 Worker 程序的 `Module.Start` 呼叫 `worker.Start()`，並把 `worker` 註冊為 Runtime 的停機 Participant。
- 若 Handler 內部需要讀寫資料庫，請在 Handler 裡明確開啟新的 `application.Command`／`Query`（Job 狀態與業務交易分離，不宣稱跨程序原子提交）。
- 停機順序：撤回 Ready → 停止認領 → Drain 等待活躍 Handler 完成 → 逾時取消並等有界清理結束 → 以有效 lease token 交還未完成租約 → 最後關閉 DB。

### 取消、死信重送與清理
- 在目前 owner 的授權可寫 UOW 內呼叫 `store.Cancel(ctx, id, trace)` 或 `store.Replay(ctx, id, trace)`，必要 Audit 與狀態更新同交易提交。
- `Replay` **只接受仍在 `Deadline` 內的 `dead` 工作**（保留原 ID／owner／schema）；若 Deadline 已過，請先對帳再另建新操作，不可偷偷延長舊期限。
- `Purge` 只清理超過 `Retention` 的成功／取消終態，`dead` 紀錄一律保留供人工排查與稽核。

## 下一步

- 前往 **[觀測與健康檢查](observability.mdx)**，學會追蹤背景工作與 API 之間的完整 Trace，並監控佇列積壓告警。
- 面對滿額 429 或 Broker 斷線怎麼應急？見 **[容量、停機與恢復](operations.md)**。
