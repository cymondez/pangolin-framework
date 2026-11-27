---
title: 疑難排解（Troubleshooting）
description: 依「環境與安裝 → 模組與 DI → 交易與 UOW → 安全與租戶 → 事件與背景工作」分類的快速診斷手冊：症狀、根本原因與修復方式一次查清。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

這份手冊整理了新進人員導入 panGOlin 時最常遇到的錯誤訊息與卡關情境。你可以直接用右上角搜尋欄輸入錯誤碼（例如 `UOW_BUSY`、`SCOPE_KIND_UNSUPPORTED`、`missing provider`）快速跳轉。

---

## 一、安裝與入門環境問題

### `go run` 找不到本機套件
- **原因**：工作目錄不在自己的 `hello-pangolin` 專案，或 `go.mod` 的 `replace` 相對路徑沒有正確指向並排的 `pangolin-framework` 資料夾。
- **修復**：依[安裝頁](../getting-started/installation.md)確認兩個資料夾並排放置，並檢查 `go.mod` 中的 `replace github.com/cymondez/pangolin-framework => ../pangolin-framework`。

### `no Go files` 或執行 `go mod tidy` 後依賴消失
- **原因**：在還沒建立任何 `.go` 檔案前就執行了 `go mod tidy`，Go 會把未被引用的 `require` 清掉。
- **修復**：先依[第一個應用](../getting-started/quickstart.mdx)建立 `main.go`，再執行 `go mod tidy`。

### 設定 `APP_NAME` 後程式輸出沒變
- **原因**：環境變數只對設定它的那一個終端視窗有效，或執行路徑錯誤（執行套件現成範例時須位於 `examples/basics` 目錄）。
- **修復**：在**同一個終端**設定變數並執行：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
$env:APP_NAME = 'Reader'
go run .
Remove-Item Env:APP_NAME
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
APP_NAME=Reader go run .
```

</TabItem>
</Tabs>

---

## 二、模組裝配與依賴注入（DI）錯誤

### 啟動報 `missing provider X`
- **原因**：沒有任何可見模組提供型別 `X`。常見兩種情況：
  1. 具象型別忘了在所屬模組的 `Configure` 呼叫 `module.AddSingleton`／`AddTransient`／`AddOperation`。
  2. 跨模組介面契約忘了由**契約擁有者**呼叫 `b.Services().Export(...)`，或是消費模組的 `DependsOn()` 沒有**直接**相依該擁有者。
- **修復**：核對契約擁有者的 `Export` 與消費者的 `DependsOn()`（見[依賴注入](../fundamentals/dependency-injection.mdx)）。

### 啟動報 `constructors must return concrete values`
- **原因**：傳給 `Add`／`AddSingleton` 的建構子回傳了介面（例如 `func New() contracts.Catalog`），而非具象型別。
- **修復**：建構子改回傳具象型別（如 `*Service`）；若要公開契約，由契約擁有者呼叫 `Export`，或在其他模組以 `module.As(contract)` 明確貢獻。

---

## 三、交易與工作單元（UOW）錯誤

### 內層服務失敗，我明明 `if err != nil` 吞掉了，外層怎麼還是無法提交？
- **原因**：預設 `Required` 共用同一根交易。內層一旦失敗，根交易即被不可逆地標記為 **rollback-only**，防堵半套資料落盤。
- **修復**：若屬同一原子業務請誠實回傳錯誤讓整筆回滾；若該子步驟確實必須獨立提交且不影響主流程，改用 `Propagation: uow.RequiresNew`（見[使用交易與工作單元](../data/unit-of-work.md)）。

### 收到 `UOW_COMMIT_UNKNOWN`
- **原因**：資料庫在 Commit 回應當下斷線或逾時，無法確認資料到底有沒有落盤。
- **修復**：**絕不可換新 key 直接重送寫入！** 請保留原本的冪等 key／操作識別，改開新的已授權唯讀 UOW 依[Command 冪等與結果對帳](../data/idempotency.md)查詢結果（注意：查無結果仍不等於確定未提交）。

### `RequiresNew` 逾時、卡住或查不到父交易剛寫入的資料
- **原因**：`RequiresNew` 另取獨立連線與獨立交易（透過 `kernel/ambient` Copy-on-Write 鏈建立，不鎖定也不修改父視圖）——它**不是 Savepoint**，因此本來就看不到父交易未提交的資料；如果連線池容量不足（例如 `MaxOpen < 2` 或平行 `RequiresNew` 超過池容量）或與父交易搶同一列鎖，就會等待到逾時。另外不可把子 `child` context 混傳給父 Executor 物件（會回 `UOW_SCOPE_MISMATCH`）。
- **修復**：確認連線池有足夠餘量、避免父子更新同一筆鎖定資料列、每個執行流一律使用自己的 `work` context，並設定合理短 Timeout。

### 收到 `UOW_BUSY`、`UOW_CLOSED` 或 `UOW_RESOURCE_MISMATCH`
- **`UOW_BUSY`**：在**同一筆 UOW（同一條連線）**內未開新 Scope 就用多個 goroutine 平行下 SQL，或前一次 `Query` 忘了 `rows.Close()`／`QueryRow` 忘了 `Scan`。→ 同一筆 UOW 內循序執行並確實關閉所有 cursor；若需要多執行緒平行操作 DB，請在各 goroutine 內透過 `RequiresNew`（需等完成再結束父回呼）或 `uow.Detach(work)` 開啟各自獨立的 UOW 與連線。
- **`UOW_CLOSED`**：把回呼的 `work` context 帶出 `Within` 外（例如背景 goroutine 在父回呼已結束後繼續使用）繼續操作。→ 若為請求內平行工作請以 `WaitGroup` 等候完成；若需跨請求背景執行請改用 `uow.Detach` 搭配獨立流程，或寫入 Outbox／Jobs 執行。
- **`UOW_RESOURCE_MISMATCH`**：同一筆根交易混用了兩個不同資料庫 resource key。→ panGOlin 不承諾跨 DB 原子提交，請拆分為獨立操作或以 Outbox 協調。

---

## 四、Bookstore 環境、登入與多租戶錯誤

### Bookstore 示範環境無法啟動或找不到資料表
- **原因**：Docker Engine 未開啟、本機 ports（`15542`/`14322`/`18180`/`18181`）被占用，或尚未跑初始化步驟。
- **修復**：先檢查示範專案容器狀態；停止自己的 API／Worker 後再依[Bookstore 教學](../tutorials/bookstore.md#1-準備示範服務)重新啟動（既有書籍資料會保留；不要重設整台 Docker Engine）：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
pwsh ./examples/bookstore/scripts/demo.ps1 status
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
docker compose -p pangolin-bookstore-demo -f deploy/compose/m1.yaml ps
```

</TabItem>
</Tabs>

### API 回應 `400`、`401` 或 `403`
- **`400 SCOPE_KIND_UNSUPPORTED`**：預設 `tenant-only` Bookstore 只接受 `tenant` Kind，不會把其他 Kind 默默降級成 tenant。
- **`401 UNAUTHENTICATED`**：缺少 `Authorization: Bearer ...` 或本機教學 Token 已過 5 分鐘效期 → 重新登入換發 Token。
- **`403 PERMISSION_DENIED`**：`X-Tenant-ID` 候選 Header 與登入身分不符（跨租戶嘗試），或缺少該動作的 Permission（包含被 User 明確 Deny）。

---

## 五、事件、背景工作與容量背壓錯誤

### 書籍建立回 201，但背景資料沒有更新（或 SignalR 沒收到推播）
- **原因**：背景 Worker 未啟動、NATS `stream`/`subject`/`worker tenants` 設定不一致，或使用 `--realtime` 生成專案時忘了另外啟動 Outbox Worker。
- **修復**：確認 Worker 終端仍在運行且參數一致（Broker ACK 不代表 Consumer 完成）；SignalR 專案請確認已啟動注入 `jetstream.Route` 的 `outbox.NewWorker`（見[SignalR 即時通知](../platform/realtime.md)）。

### 收到 `JOB_CAPACITY_EXCEEDED` 或 `EVENT_CAPACITY_EXCEEDED`（HTTP 429）
- **原因**：資料庫級有界佇列已達硬上限（含保留期內的終態紀錄），系統自動啟動 Fail-closed 背壓保護資料庫。
- **修復**：呼叫端保留原冪等鍵並退避重試；維運端檢查 `pending`/`dead`/DB Pool 與 `Retention`，先排除下游阻塞讓積壓排空（絕不提早刪除還在重送去重視窗內的紀錄，見[容量、停機與恢復](../platform/operations.md)）。

### Job 卡在 `pending` 或反覆失敗進入 `dead`
- **原因**：`DueAt` 未到、`Deadline` 已過、Payload Schema 不符、當前服務 grant／Scope Provider 異常，或代理型 Job 的原使用者已被撤權。
- **修復**：對照上述條件修復後，僅針對仍在 `Deadline` 內的 `dead` 工作於目前 owner 的授權 UOW 執行人工 `Replay`（同交易寫 Audit）；若 Deadline 已過，先對帳再另建新操作（見[持久化背景工作](../platform/jobs.md)）。
