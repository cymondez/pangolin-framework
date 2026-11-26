# SignalR 即時通訊現況報告（2026-11-26）

Date：2026-11-26。審計來源 commit `326c8811c728aab8f8434a48cc58fc6b6d9a673b`（develop，`f7f370e` 的 merge；`codex/signalr-realtime` 之 worktree 版本已併入 develop）。Go `1.27.4` windows/amd64。本檔記錄 SignalR 即時通訊的實際驗證範圍與可用邊界。**2026-11-26 正式 M7 發布 Gate 已在同版 Windows／pinned Linux（含完整 race）通過並標 Verified**，來源 commit `42c2622c270d1389b9d4775abb1d584cdf75a9ad`；正式證據見 [M7 驗收紀錄](M7.md)，重現入口見 [M7 開發指南](../guides/m7-development.md)。本文的聚焦審計數字屬 `326c881` 歷史來源，正式結論以 M7 驗收紀錄為準。

**目前結論**：模組本身（Hub、SignalR Adapter、JetStream receiver、官方 client 整合）已通過 develop `42c2622c` 的完整 M0–M6＋M7 同版 Windows／Linux（含 race）累積 Gate；104 項需求（RT-001–RT-009）／593 個具名情境／三組 Profile 映射完成（`tests/contract/m7.json` `pending: []`），原始 coverage 全部 critical ≥90%。使用邊界見下文與 M7 驗收紀錄。

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

## 2026-11-26 正式 Gate 驗收更新（`42c2622c`）

上文的聚焦審計（`326c881`）保留為實作期歷史證據；當日後續發生：

- **正式 M7 Gate 執行並 Verified**：同版 Windows／pinned Linux（含完整 race）累積 M0–M6＋M7 gate 於來源 commit `42c2622c270d1389b9d4775abb1d584cdf75a9ad`（source bytes `2b35259eec8ab02fc3f3bec44e0e8eee121a64cd603700dc8b79ede993bdb85e`）通過，原始 coverage Windows 7962/8635、Linux 7965/8635，全部 critical ≥90%（`events/nats` 76/80、`modules/realtime` 281/302、`jetstream` 57/59、`signalr` 282/298）；51 條命令／平台、官方 client 三 Profile、五項 drills、client license 鎖與 §13.6 索引（593×3＝1779 列，required 1698、N/A 81）全部通過，連續兩輪 gate 重跑逐位元重現。
- **九項 pending 全數解除**：`tests/contract/m7.json` 完成全部 61 個 M7 具名子情境映射（`pending: []`），A99/disabled-scope、A100/commit-unknown、A101/unknown-reconcile、A102/broker-redelivery／node-restart／no-client-ack、A103/redaction、A104/legacy-off／cumulative-gates 各自綁定執行證據（見 `profile-evidence-m7.json` 每列的原始 artifact sha256）；先前聚焦審計的「未驗證」描述至此已被正式映射取代。
- **已知的 gate 級限制**（詳見 [M7 驗收紀錄](M7.md)）：五項 drills 一次執行於 Windows 由 Linux 採用；race log 無個案明細（工具鏈輸出限制，套件級 race＋0 skip 為記錄證據）；demo key 僅 loopback；同日較早的 `1d7a8ce7…` gate 因驗證期間重寫證據的 `validate_report` 缺陷宣告為歷史紀錄、不構成 Verified，已在 `42c2622c` 來源以純推導＋唯讀驗證修正。
- 本檔聚焦審計的其餘邊界仍適用：`--realtime` 生成專案的 API 不運作 Outbox worker（部署需自行以 `outbox.NewWorker`＋`jetstream.Route` 接上 dispatch）；race／證據數字以 `42c2622c` 的 M7 驗收紀錄為準。