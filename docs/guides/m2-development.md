# M2 開發與驗證

在 `codex/m2-unit-of-work` 實作，產品版本固定 0.1.0。正式交付範圍以里程碑規劃 M2 為準；133 個具名子情境映射於 `tests/contract/m2.json`。開發中報告不等於 Verified。

## 隔離服務

先盤點 Docker／Compose／ports／volumes，確保專用名稱與 15542／14322／18180 未占用。不影響既有 docs 或其他服務。使用固定 Go 1.27.4、PostgreSQL 18.7、NATS 2.15.3 與 sqlc v1.31.1，版本與 digest 在 `deploy/toolchain.json`。

從倉庫根目錄執行：

```powershell
$env:GOMODCACHE = Join-Path $PWD '.cache/go-mod'
$env:GOTMPDIR = Join-Path $PWD '.cache/go-tmp'
$env:GOBIN = Join-Path $PWD '.cache/bin'
New-Item -ItemType Directory -Force .cache/go-mod,.cache/go-tmp,.cache/bin,artifacts | Out-Null
go mod download all
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
./scripts/build-test-idp.ps1
docker compose -p pangolin-m2-dev -f deploy/compose/m1.yaml up -d --build --wait --wait-timeout 60
$env:PANGOLIN_M1_ADMIN_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_APP_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_WORKER_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_NATS_URL = 'nats://127.0.0.1:14322'
$env:PANGOLIN_M1_ISSUER = 'http://127.0.0.1:18180'
./scripts/check.ps1 -Project pangolin-m2-dev
```

M1 環境變數名稱沿用以執行累積回歸；不表示使用舊服務。上述密碼僅用於公開 disposable fixture。首次以 M1 測試建立 tenant-only Bookstore schema；ScopeAware schema 僅由 M2 組織薄片安裝，不由 API 啟動執行 DDL。既有 fixture 的 schema 變更見 [API／schema 變更說明](../release-notes/m2.md)。

## Gate 與證據

`scripts/m2.py verify` 重用 M1 驗證器，執行 M0／M1／M2 工程反例、build／vet／依賴與 license、Unit、真實 Integration、分程序 API／Worker E2E、sqlc 再生成，以及同 build 的 coverage 聯集。整體與關鍵 package 各自至少 90.00%；fail／skip／缺漏阻擋驗收。Linux 加 `--race`，包含真實服務。

Windows 與 Linux 各自從同一乾淨 commit 執行，保存 `artifacts/<commit>/m2/<platform>/report.json`、raw profiles、命令 logs、go-list、依賴與 exclusions。Linux 使用 `deploy/toolchain.json` 固定 OCI Go runner 與自身 module/build cache；不能混合平台 profiles。之後執行：

```powershell
python scripts/m2.py gate --commit <完整來源 SHA>
```

Gate 核對相同 commit／source hash、全部 M1／M2 必跑案例、Linux race、E2E 與每份 artifact hash。只有通過後才在里程碑規劃標 Verified。公開 Docusaurus 使用文件在驗收完成後更新，不將內部 Gate 資料放進使用導覽。

## 實作邊界

UOW 支援完整選項解析、Required／RequiresNew、同根資源識別、deadline、SaveChanges flush、Panic／清理、可識別 Outcome 與隔離觀察器。Resource 可提供 Flush／BeforeCommit／Prepare／Release 階段；Manager 使用私有 completion context 依序完成，BeforeCommit 供事件／稽核 Adapter 組裝，完整原子事件流程仍在 M3 驗收。非交易唯讀僅供不使用 transaction-local RLS 的 Resource；PostgreSQL Adapter 明示需要交易。

M2 organization 是單一可選 capability／Authority Port，完整 Management、預設持久化治理、撤權 admission、遷移與替換資料對帳仍在 M3。ScopeAware Bookstore 保存完整 owner／Outbox，尚未提供組織事件 Worker 的可靠配送矩陣。

清理前核對 project labels，只清除本次建立的環境：

```powershell
docker compose -p pangolin-m2-dev -f deploy/compose/m1.yaml down --volumes --rmi local
```

保留 workspace logs／coverage，既有供使用者瀏覽的 docs 服務繼續運行，不做全域 prune。
