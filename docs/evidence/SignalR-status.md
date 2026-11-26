# SignalR 即時通訊現況報告（2026-11-26）

日期：2026-11-26。來源 commit `326c8811c728aab8f8434a48cc58fc6b6d9a673b`（develop，`f7f370e` 的 merge；`codex/signalr-realtime` 之 worktree 版本已併入 develop）。Go `1.27.4` windows/amd64。本檔記錄該來源實際驗證結果與可用邊界；M7 正式發布 Gate 未通過，狀態不是 Verified，見結尾「未完成驗收」。

**目前結論**：模組本身（Hub、SignalR Adapter、JetStream receiver、官方 client 整合）在此來源已實際驗證可用；M7 完整驗收（原始 coverage 計數、全庫累積 Gate、九項 pending 情境）未完成，不得視為 Verified。

## 實際可用範圍

- `modules/realtime`（Hub、授權、發布、notifications schema、Root Module 生命週期）、`modules/realtime/signalr`（ASP.NET Core JSON Hub Protocol v1／WebSocket／negotiate endpoint）、`modules/realtime/jetstream`（`Route` 分流與 per-process `Receiver`）、`events/nats` 的 `ConnectLive`／`SubscribeLive`（realtime 用 DeliverNew consumer）在 HEAD 編譯、單元與聚焦整合全部通過。
- 官方 `@microsoft/signalr` 10.0.0 client 整合測試三組 Profile 全通過（tenant-only／organization-enabled／replacement-contract），每組由**兩個獨立 server process** 驗證跨節點 fan-out、tenant／scope 隔離、multi-membership、退訂、rollback 不推送、撤權重連被拒與心跳。
- 生成面：CLI `new`（含／不含 `--realtime`）兩舊新 Profile 生成回歸通過；含 `--realtime` 的專案會另外 build 通過（organization-enabled 與 tenant-only 依 `TestGeneratedRealtimeProfiles`）。

**生成專案的重要邊界**：`--realtime` 範本的 API 會啟動 per-node receiver 並連線 JetStream（`internal/app/realtime.go.tmpl`），但生成專案的 API **不運作 Outbox worker**。要讓應用推送的事件真的送達 client，必須由部署另行提供／設定一個以 `jetstream.Route` 注入的 Outbox worker dispatch（如 `examples/realtime` 在 `startIngress` 內以 `outbox.NewWorker`＋`jetstream.Route` 完成的做法）；`assets/realtime/REALTIME.md.tmpl` 已明示「The generated API does not operate an Outbox worker」。未接上 worker 時事件停在資料庫 Outbox，畫面上的 client 不會收到推播。

## 前置條件（可照做）

以倉庫根目錄執行。連接埠先用 `Get-NetTCPConnection -State Listen` 確認空閒（本輪使用 27121／27122）；`$project` 是本次專屬的 Compose project 名，換值即可隔離不同輪次：

```powershell
$project = 'pangolin-signalr-audit-20261126'
$env:PANGOLIN_PG_PORT = '27121'
$env:PANGOLIN_NATS_PORT = '27122'
docker compose -p $project -f deploy/compose/compose.yaml up -d --wait postgres nats
$env:PANGOLIN_REALTIME_ADMIN_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:27121/pangolin_m0?sslmode=disable'
$env:PANGOLIN_REALTIME_APP_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:27121/pangolin_m0?sslmode=disable'
$env:PANGOLIN_REALTIME_WORKER_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:27121/pangolin_m0?sslmode=disable'
$env:PANGOLIN_REALTIME_NATS_URL = 'nats://127.0.0.1:27122'
$env:PANGOLIN_REALTIME_DEMO_KEY = 'example-loopback-only-key'
go run ./examples/realtime --provision
```

- `--provision` 必須先跑：資料庫 events migration 與 demo schema 未建立時，真實 PG 的 publish 測試會以 `UOW_ROLLED_BACK` 失敗（本輪實際踩到；供應後即通過，非程式缺陷）。
- 整合測試以子程序繼承環境變數，`PANGOLIN_REALTIME_ADMIN_DSN` 也要設，否則每次 `--provision` spawn 直接退出（本輪初跑因此 3/3 fail，補上後 3/3 pass）。
- 結束後以 `docker compose -p $project -f deploy/compose/compose.yaml down --volumes` 清除同一 `$project` 的容器／network／volumes。

## 驗證命令與結果（本輪實跑）

| 驗證 | 命令 | 結果 |
|---|---|---|
| build | `go build ./...` | PASS |
| vet | `go vet ./modules/realtime/... ./events/nats/... ./internal/scaffold/...` | PASS |
| CLI 兩 Profile 生成回歸 | `go test -count=1 ./internal/scaffold/...` | PASS（16.1s） |
| 契約盤點 | `go run ./tools/check --root .` | 95 requirements 構造有效 |
| 聚焦 coverage（有真實服務） | `PANGOLIN_REALTIME_APP_DSN`／`PANGOLIN_REALTIME_NATS_URL` 下 `go test -count=1 -cover ./modules/realtime/... ./events/nats/...` | realtime 92.7%、jetstream 93.2%、signalr 93.6%、events/nats 92.5%（PASS） |
| 聚焦 coverage（純單元） | 同上但無環境變數 | realtime 86.1%、jetstream 23.7%、signalr 93.6%、events/nats 66.2%（publish/live 測試 skip，**只供對照，不達 90% 門檻**） |
| 官方 client 整合 | `PANGOLIN_REALTIME_BINARY`＋各 DSN 後 `npm.cmd --prefix examples/realtime/client test` | 3/3 PASS |
| 聚焦 race（pinned Linux） | compose 疊加 `artifacts/signalr-audit-20261126/race-compose.yaml`（golang `1.27.4-bookworm@sha256:4fa7c968…`）跑 `go test -count=1 -race ./modules/realtime/... ./events/nats/...` | 4/4 套件 `ok` |

注意：

- 上述 coverage 是 `go test -cover` 的**四捨五入百分比**，屬聚焦套件組合（`modules/realtime/...`＋`events/nats/...`）；本次**未保存原始 statement count profile**，原始計數不適用於本輪，全倉庫 release coverage 與 §13.3 分平台聯集門檻皆未收集。歷史報告（如 P3）若宣稱「原始計數」，屬其自身來源的舊量測，不代表本輪。
- race 結果為**套件層級 PASS**：log 無案例明細，且真實服務案例（publish／live）在 race 容器內仍依環境變數啟用、可能 skip；**本輪未盤點 skip 數，不宣稱 race 中零 skip 或每個 live 情境都完成**。真實服務情境的完成證據以整合測試與非 race coverage 執行為準。
- 整合測試 3/3 是每組 Profile 內的完整情境序列；兩個 server process 由測試以 `--stop-on-stdin-eof` 自管生命週期，race 執行不含整合情境。

## 服務與環境

- Compose project `pangolin-signalr-audit-20261126`：`postgres:18.7-bookworm@sha256:194e55feb2597b1465dbee81cc9fd8d204566cd2eb42c8d22fd6c931a38781b1`、`nats:2.15.3-alpine@sha256:558394a23dd3c36f2d3a64b51865fd09908010e80a542cea5e75c2af543ed62a`（與 `deploy/compose/compose.yaml` pin 一致），綁定 `127.0.0.1:27121`／`:27122`，專屬 volumes／network，結束後已以 `docker compose down --volumes` 精確清除。
- pinned race runner 以 `docker compose run` 執行（image `golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea`；`docker run` 需另核准，`--add-host host.docker.internal:host-gateway` 解析宿主服務）。
- 官方 client 依 `examples/realtime/client/package.json` 固定 `@microsoft/signalr` `10.0.0`（lock 安裝 `--ignore-scripts`）。
- 日誌保存於 `artifacts/signalr-audit-20261126/logs/`（build、vet、coverage 兩種情境、provision、整合、race、compose up）及 `race-compose.yaml`。`artifacts/…` 目錄為 gitignore 範圍，僅作本機證據。

## 未完成驗收（剩餘缺口）

- **M7 正式發布 Gate 未執行**：未跑 `scripts/check.ps1`／`check.sh` 同版累積發布流；原始 statement count profile 未保存，全倉庫 race 與 §13.3 分平台 coverage 聯集未收集。
- 未盤點聚焦測試在 race／coverage 執行中的 skip 明細（env-gated 案例在缺服務時 skip）；「無 skip」未經證實，不作宣稱。
- `tests/contract/m7.json` 9 項 pending 均仍未滿足；**三項 pending 理由已依現有證據更正**：A102/broker-redelivery（真實 JetStream 去重與 poison Term 已驗證；未 ACK 依 AckWait 的同節點重送尚未驗證）、A99/disabled-scope（連線中 active scope 在權威端停用的即時撤權語意未驗證）與 A104/legacy-off（no-realtime 生成變體已驗證，僅缺舊入口啟動對照整合）。其餘項目理由不變。
- A104/legacy-off（原因見上）；A104/cumulative-gates 屬發布階段；A103/redaction 依 observability 共用遮罩待 Gate 審查；A100/commit-unknown、A101/unknown-reconcile、A102/node-restart／no-client-ack 需真實 DB／Broker 故障注入或節點生命週期操作，本輪未提供。
- 未更新任何 M0–M6 Verified 狀態；本檔與里程碑規劃書 §9 的 In progress 標記僅描述此來源的聚焦證據。