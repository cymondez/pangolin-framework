# M7 SignalR Realtime — 最終報告

**Scenario:** panGOlin 可選 SignalR 即時通訊模組（RT-001–RT-009／A96–A104）之實作、整合測試與驗收證據整併
**Outcome:** ✅ **2026-11-26 正式 M7 並行發布 Gate 已 Verified**（develop 來源 commit `42c2622c270d1389b9d4775abb1d584cdf75a9ad`，同版 Windows／pinned Linux 含完整 `-race`；見第六節與 [M7 驗收紀錄](../../docs/evidence/M7.md)）。**先前審計結論（歷史紀錄）：模組在 develop `326c881` 已實際驗證可用，M7 完整驗收當時未完成、不標 Verified**
**Branch:** `codex/signalr-realtime`（HEAD `c868bfd`，全部變更為工作樹未提交狀態——**此分支／未提交快照屬歷史來源**；實作其後以 `f7f370e` 併入 develop，現行審計來源為 `326c881`，正式驗收來源為 `42c2622c`）

> **2026-11-26 補正**：本報告撰寫時的來源邊界如上（worktree 未提交）。該實作後以 `f7f370e` 提交並併入 develop（HEAD `326c881`）。2026-11-26 於 develop `326c881` 重新執聚焦審計：build／vet／契約盤點／兩種 CLI 生成 Profile 回歸／官方 client 整合 3/3／pinned Linux race（4 套件）／聚焦 coverage 百分比（env-gated）92.7/93.2/93.6/92.5 全數通過；**未保存原始 statement count profile，原始計數與全倉庫 release coverage 未收集**；詳見 [SignalR 現況報告](../../docs/evidence/SignalR-status.md)。下文歷史敘述保留撰寫時點的來源，不回溯改寫。
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

### P3 聚焦量測（歷史；撰寫於 M7 工作樹時期，非 2026-11-26 審計）

```
go test -cover -count=1 ./modules/realtime/... ./events/nats/...
```
4 套件全 PASS：realtime 92.7%、signalr 93.6%、jetstream 93.2%、events/nats 92.5%（撰寫時點的原始計數宣稱屬該次歷史量測；該 profile 未隨報告保存，2026-11-26 審計僅重現四捨五入百分比，未重收集原始計數；~2 語句不可達殘餘見 results/p3.md）。

> 上列數字需要 `PANGOLIN_REALTIME_APP_DSN`／`PANGOLIN_REALTIME_NATS_URL` 指向真實服務才有意義：publish／live 案例在缺變數時 skip，同命令的純單元結果為 realtime 86.1%、jetstream 23.7%、signalr 93.6%、events/nats 66.2%（2026-11-26 於 develop `326c881` 實測對照）。jetstream 與 events/nats 的覆蓋幾乎全部來自真實 JetStream／PG 案例，引用時必須連同前置條件一併引用。

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

### 9 項 pending 情境（m7.json 已註冊並附理由，未虛構映射；2026-11-26 起三項理由已依證據更正，情境本身仍 pending）

- A99/disabled-scope：需連線中 active scope 的權威端狀態停用（live provider revision／state 拒絕後續訂閱／配送）
- A100/commit-unknown、A101/unknown-reconcile：需真實 DB／Broker 故障注入與提交結果不確定狀態模擬，超出單元與既有 client 整合能力
- A102/broker-redelivery：需未 ACK 訊息對同節點 consumer 依 AckWait 重送且不越過有界去重的 Broker 觀測；A102/node-restart、A102/no-client-ack：需節點／Profile 生命週期操作（重啟、kill、手動 ack 抑制）
- A103/redaction：需 observability 共用套件提供遮罩驗證點
- A104/legacy-off：no-realtime 生成變體已驗證，僅缺無 realtime Module 的舊入口啟動對照整合；A104/cumulative-gates：屬發布 Gate／累積回歸階段，非模組實作層

### 其他

- race 於 Windows host 未執行（缺 cgo 工具鏈），race 證據以 pinned Linux 為準
- compose project `pangolin-realtime-example`（PG 26000／NATS 26001）保留運行，由主代理統一處置；NATS 伺服器餘先前 session 的 `RT_*` 串流（P3 測試串流已於 cleanup 自刪）。**2026-11-26 補正**：該 project 已不在執行中；本輪聚焦審計改用獨立 project `pangolin-signalr-audit-20261126`（PG 27121／NATS 27122），結束後已 `down --volumes` 清除。
- 殘留的 `pangolin-signalr-1a215bc-*` 容器（port 25342／24222）為先前 session 存留，本次未動

---

## 四、尚未通過的正式發布 Gate

- **M7 正式發布 Gate 未執行**：未跑完整累積發布流（`scripts/check.ps1`／`check.sh` 同版 Windows／Linux 全量 Gate），本輪僅完成 M7 範圍的單元＋整合＋race／vet／coverage 與契約註冊
- **Linux 全量 race 與 §13.3 分平台 coverage 未收集**：僅對 `modules/realtime/...`、`events/nats/...` 做 pinned Linux race；全庫 race 對帳與分平台 coverage 聯集未收集
- **`artifacts/design-review-2026-11-24` 重現 6 項失敗**：TestLocalSwallowedNestedBusyCompletesRoot、TestLocalPropagatedNestedBusyFailsRoot、TestDIResolveHoldsContainerMutexAcrossFactory、TestDIOpenScopeWaitsForGlobalMutexHeldByFactory、TestDICloseHoldsContainerMutexAcrossCleanup、TestJWTRequestsJWKSForEveryValidShapeBeforeSignatureRejection —— 為 2026-11-24 設計審查輪的凍結重現目錄，kernel/di、persistence/uow、security/authn 在本工作樹未變（相關路徑 `git diff HEAD` 為空），失敗屬 branch 基線 `c868bfd` 合併後既有狀態，**非 M7 本次引入**，標記為需另行確認的限制
- **未更新任何既有 Verified 狀態**：M0–M6 的 Verified 與所有 milestone 狀態維持不動；本輪證據只適用於本分支工作樹與 M7 情境，不取代發布驗收

---

## 五、2026-11-26 聚焦審計增補（develop `326c881`）

- `go build ./...`、`go vet`（realtime／nats／scaffold）、`go run ./tools/check`（95 requirements 盤點）、`go test ./internal/scaffold/...`（含無 `--realtime` 舊 Profile 與 `--realtime` 新 Profile 生成回歸）全部通過。
- 初跑 coverage 曾見 `TestRealtimePublishStagesOutboxEvent` `UOW_ROLLED_BACK`：全空 PG 未跑 `--provision` 所致（examples/realtime `--provision` 建立後通過），非 production 缺陷；整合測試初跑 3/3 fail 亦因未設 `PANGOLIN_REALTIME_ADMIN_DSN`（spawn 的 provision 直接退出），補設後 3/3 pass。兩者皆為前置條件，已寫入 [evidence](../../docs/evidence/SignalR-status.md) 與範例 README。
- coverage 語意更正：上文「P3 聚焦量測」的套件百分比在 2026-11-26 審計重現為四捨五入數字（env-gated 聚焦套件組合），本次未保存原始 statement count profile，原始計數不適用於本輪；純單元（無真實服務）下 jetstream 23.7%／events/nats 66.2%，引用覆蓋不得與真實服務前置條件切割；且此聚焦組合不等於全倉庫 release coverage（§13.3 聯集未收集）。race 結果為套件層級 PASS，log 無案例明細且 env-gated 案例可能 skip；skip 明細未盤點，不宣稱零 skip。
- pending 理由更正（`tests/contract/m7.json`）：A102/broker-redelivery（未 ACK 對同一節點 consumer 依 AckWait 重送、不越過有界去重——非另一 process）、A99/disabled-scope（連線中 active scope 的權威端狀態停用，需 live provider revision／state 證據）與 A104/legacy-off 的現況已依現有證據改寫；九項情境本身仍保持 pending，未虛構映射。
- 生成專案邊界：`--realtime` 範本 API 啟動 per-node receiver，但不運作 Outbox worker；端到端推送需部署提供以 `jetstream.Route` 注入的 worker。
- 完整現況、可照做命令與剩餘缺口見 [docs/evidence/SignalR-status.md](../../docs/evidence/SignalR-status.md)。

---

## 六、2026-11-26 正式 M7 Gate（Verified）

同版 Windows／pinned Linux（含完整 `-race`）累積 M0–M6＋M7 gate 在 develop 來源 commit `42c2622c270d1389b9d4775abb1d584cdf75a9ad`（source bytes `2b35259eec8ab02fc3f3bec44e0e8eee121a64cd603700dc8b79ede993bdb85e`）通過，`scripts/m7.py gate` 寫入 `artifacts/42c2622c270d1389b9d4775abb1d584cdf75a9ad/m7/gate.json`；連續兩輪 gate 重跑證明每個 artifact／雜湊／mtime 逐位元不變（唯讀驗證），並獨立重算所有 report-bound artifact 與命令 log hash，零不符。

- 每平台 51 條命令；M0 盤點（104 群組／593 具名子情境）、m0–m7 Python 回歸、unit＋real-service integration（無 skip）、M1 E2E、M5 模組／CLI coverage、官方 `@microsoft/signalr` 10.0.0 三 Profile E2E（兩獨立儀器化節點）、18 套件 client license 鎖、sqlc v1.31.1、五項發布 drills（Windows inline、Linux host-run 同源採用）。
- 原始 atomic 聯集 coverage：Windows 7962/8635（92.21%）、Linux 7965/8635（92.24%）；全部 28 個 critical package ≥90%（最低 90.26%；`events/nats` 76/80、`modules/realtime` 281/302、`jetstream` 57/59、`signalr` 282/298）。
- §13.6 索引：593×3＝1779 列（required 1698、N/A 81），`tests/contract/m7.json` `pending: []`（61 個 M7 具名子情境全部映射、closure M1..M7＝147/134/185/28/25/13/61）。
- 第五節所列九項 pending 至此全數解除（各列綁定原始 artifact sha256）；「M7 正式發布 Gate 未執行」等缺口不再存在。
- 已知限制（誠實記錄，詳見 [M7 驗收紀錄](../../docs/evidence/M7.md)）：drills 一次執行於 Windows 由 Linux 採用；race log 無個案明細（套件級 race＋0 skip 為記錄證據）；demo key 僅 loopback；同日較早的 `1d7a8ce7…` gate 因 `validate_report` 驗證期間重寫證據（主機換行恆真比較）宣告為歷史紀錄、不構成 Verified，修正後於 `42c2622c` 重驗通過。
- VERSION 維持 `0.1.0`，realtime 子系統目標 v0.2，**非 tag release**；沒有 push／tag／部署行為。第五節審計數字（聚焦百分比、env-gated skip 未盤點等）屬 `326c881` 歷史來源，正式結論以本節與 [M7 驗收紀錄](../../docs/evidence/M7.md) 為準。