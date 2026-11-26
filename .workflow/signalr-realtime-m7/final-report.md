# M7 SignalR Realtime — 最終報告

**Scenario:** panGOlin 可選 SignalR 即時通訊模組（RT-001–RT-009／A96–A104）之實作、整合測試與驗收證據整併
**Outcome:** ✅ 工作包範圍完成（M7 正式發布 Gate 未執行，見「尚未通過的正式發布 Gate」）
**Branch:** `codex/signalr-realtime`（HEAD `c868bfd`，全部變更為工作樹未提交狀態）
---

## 一、實際完成的功能

### 新增模組

**`modules/realtime/`**（SignalR hub、授權、發佈、JetStream 配送）

- 根套件：hub、gateway、publisher、notifications schema、module 生命週期（Root Module 組裝）
- `modules/realtime/signalr/`：HTTP/ negotiable endpoint、token 綁定、協定 flow（單線 SignalR 語意，非 SSE/WebSocket 包裝）
- `modules/realtime/jetstream/`：以 JetStream fan-out 到 session；Quiesce／Drain／Close 有界停機

**`events/nats/fanout.go`**：live subscription 層（durable consumer、Ack／Reject／Next、retention 檢查）

**`examples/realtime/`**：CLI 入口 + 官方 `@microsoft/signalr` 10.0.0 整合測試（`client/integration.test.mjs`，兩獨立 server process、三組 Profile、真實 PG 26000／NATS 26001）

**`internal/scaffold/`**：`realtime`／`no-realtime` 兩種 Profile 範本資產與 CLI 生成流程（`cli_realtime_test.go` 回歸）

### 修改檔案（git status --short 實際範圍）

| 區域 | 檔案 |
|---|---|
| Broker | `events/nats/broker.go`、README |
| Scaffold | `internal/scaffold/cli.go`、`assets/common/*`、`assets/runtime/*`（含 app.go、foundation、go.mod、runtime-manifest、events/nats、observability 模板）、新增 `assets/no-realtime/`、`assets/realtime/`、`assets/runtime/modules/realtime/` |
| 組裝 | `kernel/app/app.go`、`observability/telemetry.go` |
| 文件 | 宣告 `docs/site/content/platform/realtime.md`（M7 新增，教學導覽）；既有 README 同步（`modules/realtime/README.md` 為新檔案一併於工作樹維護） |
| 規範 | `design/panGOlin-framework_實作規範書.md`、`design/panGOlin-framework_里程碑規劃書.md` |
| 檢查 | `internal/quality/contract.go`（校驗 RT-001..009 ↔ A96–A104）、`tools/check/main.go`、`cmd/pangolin/README.md`、`docs/site/sidebars.js`、`go.mod` |

（僅列出模式與關鍵檔案；完整清單見 `git status --short`。production 行為修改集中在 realtime／events-nats／scaffold/kernel-app 組裝，其餘多為模板與文件同步。）

**一次性工具（未 commit，於 `artifacts/`）**：`m7-append-subtests.py`、`m7purge.go`。

**文件新增**：`.workflow/signalr-realtime-m7/` 全部產物、`docs/site/content/platform/realtime.md`。

---

## 二、實際通過的驗證

### 本輪確認（2026-11-26 實跑）

```
go test -cover -count=1 ./modules/realtime/... ./events/nats/...
```
4 套件全 PASS：realtime 92.7%、signalr 93.6%、jetstream 93.2%、events/nats 92.5%（原始計數，皆達 §13 90% 門檻；~2 語句不可達殘餘見 results/p3.md）。

### 已由 P1–P3 核對的命令與結果（引用，未重跑重量項）

| 驗證 | 命令（摘要） | 結果 |
|---|---|---|
| build | `go build ./...` | PASS |
| 全庫單元回歸 | `go test ./...` | production 套件全綠；唯一失敗在 `artifacts/design-review-2026-11-24`（6 項歷史重現，見第四節） |
| 官方 client 整合測試 | `npm --prefix examples/realtime/client test`（真實 PG／NATS、兩節點、tenant-only／organization-enabled／replacement-contract） | 3/3 PASS |
| race | pinned `golang:1.27.4-bookworm`（docker）`go test -race ./modules/realtime/... ./events/nats/...` | 4 套件 PASS（Windows 本機缺 gcc 不執行） |
| vet | `go vet ./modules/realtime/... ./events/nats/...` | 無錯誤 |
| 契約 | `tests/contract/m7.json`（35 unit + 17 template 映射、9 pending 附理由）、`subtests.json` 增 A96–A104 共 61 情境（diff 純新增）；`internal/quality/contract.go` 校驗通過 | 註冊完成 |
| scaffold 回歸 | `go test ./internal/scaffold/... -count=1` | PASS（含兩種 Profile 生成） |
| JSON 契約解析 | PowerShell `ConvertFrom-Json` 驗 subtests.json／m7.json | PASS |

---

## 三、未支援／未執行的項目

### 9 項 pending 情境（m7.json 已註冊並附理由，未虛構映射）

- A99/disabled-scope、A100/commit-unknown、A101/unknown-reconcile：需真實 DB／Broker 故障注入與提交結果不確定狀態模擬，超出單元與既有 client 整合能力
- A102/broker-redelivery、A102/node-restart、A102/no-client-ack：需 broker 重送與節點／Profile 生命週期操作（重啟、kill、手動 ack 抑制）
- A103/redaction：需 observability 共用套件提供遮罩驗證點
- A104/legacy-off、A104/cumulative-gates：屬發布 Gate／累積回歸階段，非模組實作層

### 其他

- race 於 Windows host 未執行（缺 cgo 工具鏈），race 證據以 pinned Linux 為準
- compose project `pangolin-realtime-example`（PG 26000／NATS 26001）保留運行，由主代理統一處置；NATS 伺服器餘先前 session 的 `RT_*` 串流（P3 測試串流已於 cleanup 自刪）
- 殘留的 `pangolin-signalr-1a215bc-*` 容器（port 25342／24222）為先前 session 存留，本次未動

---

## 四、尚未通過的正式發布 Gate

- **M7 正式發布 Gate 未執行**：未跑完整累積發布流（`scripts/check.ps1`／`check.sh` 同版 Windows／Linux 全量 Gate），本輪僅完成 M7 範圍的單元＋整合＋race／vet／coverage 與契約註冊
- **Linux 全量 race 與 §13.3 分平台 coverage 未收集**：僅對 `modules/realtime/...`、`events/nats/...` 做 pinned Linux race；全庫 race 對帳與分平台 coverage 聯集未收集
- **`artifacts/design-review-2026-11-24` 重現 6 項失敗**：TestLocalSwallowedNestedBusyCompletesRoot、TestLocalPropagatedNestedBusyFailsRoot、TestDIResolveHoldsContainerMutexAcrossFactory、TestDIOpenScopeWaitsForGlobalMutexHeldByFactory、TestDICloseHoldsContainerMutexAcrossCleanup、TestJWTRequestsJWKSForEveryValidShapeBeforeSignatureRejection —— 為 2026-11-24 設計審查輪的凍結重現目錄，kernel/di、persistence/uow、security/authn 在本工作樹未變（相關路徑 `git diff HEAD` 為空），失敗屬 branch 基線 `c868bfd` 合併後既有狀態，**非 M7 本次引入**，標記為需另行確認的限制
- **未更新任何既有 Verified 狀態**：M0–M6 的 Verified 與所有 milestone 狀態維持不動；本輪證據只適用於本分支工作樹與 M7 情境，不取代發布驗收
