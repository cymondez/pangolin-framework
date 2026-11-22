# M1 開發與驗證

本頁保留 M1 歷史來源的重現流程；現行 check.ps1／check.sh 已累積至 M2，請依 [M2 指南](m2-development.md) 操作。單獨重現 M1 Gate 請使用驗收紀錄所列歷史來源。

M1 在 `codex/m1-vertical-flow` 實作。核心為 module DAG／SemVer、跨模組 lifecycle、DI、四層唯讀組態與有界 Drain；Bookstore 提供 tenant-only 的 Required 交易、RS256 JWT／JWKS、PostgreSQL FORCE RLS，以及分程序 Outbox → JetStream → Inbox → Read Model。完整 UOW、安全及事件矩陣仍依 M2／M3 交付。

各模組在自身目錄附 `README.md`，從根目錄的模組表或 [Bookstore 分層索引](../../examples/bookstore/README.md) 進入。`tests/contract/m1.json` 列出各模組與工具／測試索引的說明文件，Gate 檢查其存在與本機連結；開發規範要求實作與文件同一變更完成。

## 工具與隔離環境

Go、服務 image digest 與 sqlc 版本由 `deploy/toolchain.json` 固定；Go clients 與完整依賴／license 雜湊在 `go.mod`、`go.sum`、`deploy/dependencies.json`。先依 AGENTS.md 唯讀盤點 Docker／Compose／volumes／ports／資源，再確認 `pangolin-m1-dev` 及 15542／14322／18180 未被其他工作使用。不要啟停共用 Engine。

```powershell
$env:GOMODCACHE = Join-Path $PWD '.cache/go-mod'
$env:GOTMPDIR = Join-Path $PWD '.cache/go-tmp'
$env:GOBIN = Join-Path $PWD '.cache/bin'
New-Item -ItemType Directory -Force .cache/go-mod,.cache/go-tmp,.cache/bin,artifacts | Out-Null
go mod download all
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
./scripts/build-test-idp.ps1
docker compose -p pangolin-m1-dev -f deploy/compose/m1.yaml config --quiet
docker compose -p pangolin-m1-dev -f deploy/compose/m1.yaml up -d --build --wait --wait-timeout 60
```

本機服務只綁 loopback；PG／NATS／IdP 記憶體上限分別 256／128／64 MiB，每服務 0.5 CPU。使用自己的 volume／network；可透過 `PANGOLIN_M1_PG_PORT`、`PANGOLIN_M1_NATS_PORT`、`PANGOLIN_M1_IDP_PORT` 選其他 ports，並同步以下連線值。資料庫名沿用 M0 的 disposable role fixture `pangolin_m0`，不表示共用 M0 的容器或 volume。

## 執行累積 Gate

以下密碼只用於 Compose 內已公開的 disposable fixture。正式環境須由 Secrets 注入，不能沿用。

```powershell
$env:PANGOLIN_M1_ADMIN_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_APP_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_WORKER_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_NATS_URL = 'nats://127.0.0.1:14322'
$env:PANGOLIN_M1_ISSUER = 'http://127.0.0.1:18180'
./scripts/check.ps1 -Project pangolin-m1-dev
```

`scripts/m1.py verify` 執行 API／依賴／license／import 檢查、build／vet、全部 M0 與 M1 工程反例、Go unit／真實 Integration、插樁 API／Worker 分程序 E2E、sqlc 再生成比對，以及同一 build 的 coverage block 聯集。關鍵 package 各自及整體低於 90% 均失敗；已交付測試 fail／skip／缺漏均失敗。Linux 加 `--race`，包含真實 Integration；Windows 無 C compiler 時不假報 race，由 Linux runner 提供獨立證據。

測試只用自己建立的資料，隨機資源 ID 保留 owner 與 EventID；重跑先排空前次測試的 pending Outbox。S2 用 DB trigger 在第二次 SQL 寫入注入錯誤，核對業務資料不存在；S3 的 testkit Worker 在真實 Broker publish ACK 後、mark sent 前以 exit 17 崩潰，正式 Worker 重啟後同 EventID 的 Outbox attempts=2、Inbox processed=1。Test IdP 與 crash-worker 都是 fixture，不能部署。

開發工作樹的 report 記錄 commit、source hash 與 dirty 狀態，只是 development evidence。乾淨 commit 的 Windows／Linux report 分別保存在 `artifacts/<commit>/m1/<platform>/`；兩份完整 report 與 Linux race 都通過後執行 `python scripts/m1.py gate --commit <full-sha>`。Gate 核對相同來源、必跑案例、raw profiles、E2E 結果及每份 log／artifact 的 hash。狀態文件不得在此之前寫 Verified。

`tests/contract/m1.json` 將 47 個 M1 具名子情境映射到執行案例；原 A01–A95／三種 Profile 文件仍是完整產品計畫，不能因 M1 通過將 M2–M6 標為完成。M1 核心案例與組織 Provider 無關；薄流程只交付 tenant-only，組織與 replacement 的完整符合性留待原里程碑。

GitHub Actions 的 Linux job 執行完整 M1 Gate，Windows hosted job 執行 native portability tests；完整 Windows DB／E2E／coverage 證據另需有 Docker Desktop 的 Windows runner，跨平台 milestone gate 不接受缺少該 report。遠端 workflow 必須實際執行後才算遠端證據。

## 手動運行 Bookstore

Integration harness 首次建立 M1 schema／Broker topology。這是 M1 fixture bootstrap；正式部署 migration／鎖與相容性矩陣於 M3 驗收。API 啟動只檢查 schema，不執行 DDL。

設定 `PANGOLIN_DSN` 為 app DSN、`PANGOLIN_ISSUER` 為可信 issuer 後執行 `go run ./examples/bookstore/cmd/api -listen 127.0.0.1:18181`。本機可由測試 IdP `/token` 取得真實簽章 token，使用 `Authorization: Bearer <token>` 呼叫 `POST /books`（id、title）及 `GET /books/<id>`。id 可由客戶端預先指定，作為回應遺失後的狀態查詢識別；查無資料不代表 Commit Unknown 已確認回滾。

另一程序設定 `PANGOLIN_DSN` 為 worker DSN、`PANGOLIN_NATS_URL`、`PANGOLIN_WORKER_TENANTS=tenant-a,tenant-b`，執行 `go run ./examples/bookstore/cmd/worker`。兩程序均支援 `-run-for 10s` 供有界 demo；一般使用 Ctrl+C／SIGTERM 停機。可信 JWKS URL 預設 issuer 下的 `/jwks.json`，可由 `PANGOLIN_JWKS_URL` 明示設定；這是 M1 RS256 切片，完整 OIDC discovery／演算法與安全矩陣仍由 M3 交付。

結束前確認 project 歸屬，再以 `docker compose -p pangolin-m1-dev -f deploy/compose/m1.yaml down --volumes --rmi local` 清除自己的三個服務、network、volumes 與專用映像，不做全域 prune。

目錄歸屬與新入口見 [倉庫目錄指南](repository-layout.md)；本次搬移的命令／import 對照見 [目錄整理說明](../release-notes/source-layout.md)。
