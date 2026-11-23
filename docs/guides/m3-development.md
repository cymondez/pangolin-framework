# M3 開發與驗證

開發分支 `codex/m3-security-reliable-events`；產品版本仍為 0.1.0。正式責任以里程碑規劃書 M3 為準，185 個具名情境位於 `tests/contract/m3.json`。工作樹的測試成功不能代替同 commit 的雙平台 Gate。

## 專用真實服務

先盤點 Docker／Compose、ports、volumes 與 labels；不要操作既有 docs、其他專案或共用 PostgreSQL。固定 Go 1.27.4、PostgreSQL 18.7、NATS 2.15.3、Goose v3.30.0、sqlc v1.31.1，完整 checksum／license 位於 deploy。Windows 從倉庫根目錄使用非 login PowerShell：

```powershell
$env:GOMODCACHE = Join-Path $PWD '.cache/go-mod'
$env:GOTMPDIR = Join-Path $PWD '.cache/go-tmp'
$env:GOBIN = Join-Path $PWD '.cache/bin'
New-Item -ItemType Directory -Force .cache/go-mod,.cache/go-tmp,.cache/bin,artifacts | Out-Null
go mod download all
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
./scripts/build-test-idp.ps1
docker compose -p pangolin-m3-dev -f deploy/compose/m1.yaml up -d --build --wait --wait-timeout 60
$env:PANGOLIN_M1_ADMIN_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_APP_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_WORKER_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_NATS_URL = 'nats://127.0.0.1:14322'
$env:PANGOLIN_M1_ISSUER = 'http://127.0.0.1:18180'
./scripts/check.ps1 -Project pangolin-m3-dev
```

公開密碼只屬於 disposable fixture。M1 環境變數名稱沿用是為執行累積回歸。各套件建立及清除自己的 `m3_*` 測試 DB，業務 SQL 使用非特權 app／worker role；bootstrap 只做部署、故障注入與獨立對帳。

## 雙平台與同版 Gate

`m3.py verify` 執行 M0/M1/M2/M3 工程反例、build／vet、Unit、真實 Integration、分程序 E2E、sqlc 再生成、依賴／license 及 statement block 聯集。整體手寫 production 與各關鍵 package 分別達 90.00%，必跑 skip／缺案例／缺 profile 均失敗。

Broker 重啟由 host file barrier 處理，只對核對過 labels 的專用 project NATS 操作；不是測試程式任意執行 Docker。Linux runner 使用同一固定 Go OCI image、原始 source bytes、獨立 cache 與相同服務 network，並跑 race。預先把 Linux sqlc v1.31.1 放在 `.cache/linux-bin/sqlc` 後：

```powershell
python scripts/m3_linux.py --project pangolin-m3-dev
python scripts/m3.py gate --commit <完整來源 SHA>
```

證據保存在 `artifacts/<commit>/m3/{windows-amd64,linux-amd64}`；兩平台分開計算 coverage，不混合分母或平均百分比。Gate 核對同一乾淨 commit／source hash、185 個 M3 情境及 M1/M2 累積案例、Linux race、E2E 與全部 artifact hash。開發中 dirty run 只供查驗，不能標 Verified。

Goose migration 一旦套用不再改寫；開發若新增較低版本 migration，不允許 `allow-missing` 掩蓋順序問題，應在確認歸屬後重建本次 disposable DB／volume。正式前向升級、原子 policy 切換、Provider 替換與非法停用都有獨立驗收，見 [測試索引](../../tests/m3/README.md) 與 [變更說明](../release-notes/m3.md)。

## 範圍與清理

Scope execution harness 供 Event／Job 以目前服務 grant 重新授權；完整持久化 Job Worker、容量／背壓與完整觀測仍屬後續範圍。NATS 測試驗證 file persistence／durable consumer／publish acknowledgement／路由失敗，可信 producer 仍需由實際部署的 Broker ACL／session 與 verifier 配合，不能信任 Envelope 自稱。

測試完成先保留 logs／coverage，核對 project labels 後清除專用容器、network、volumes、IdP 映像與 Linux cache：

```powershell
docker compose -p pangolin-m3-dev -f deploy/compose/m1.yaml down --volumes --rmi local
docker volume inspect pangolin-m3-dev-linux-cache
docker volume rm pangolin-m3-dev-linux-cache
```

只移除已確認屬於本次工作的 cache，不使用全域 prune。使用者瀏覽用的 docs 服務另外管理並保持運行。公開 Docusaurus 文件在 M3 Runtime 驗收通過後更新、建置並走查，使用者導覽不放內部里程碑與 Gate。
