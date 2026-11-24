# M6 開發與發布驗收

M6 對應 WP18，正式交付 `REL-003`／A70、`REL-005`／A72、`ORG-014`／A95。開發分支 `codex/m6-release-acceptance`；產品版本仍為 0.1.0、規範基線 v1.3。完成狀態以同一乾淨來源 commit 的雙平台 Gate 為準，不能由本指南或案例計畫推論已 Verified。

## 環境

從 repository root 操作。Go 1.27.4、PostgreSQL 18.7、NATS 2.15.3、Goose 3.30.0、sqlc 1.31.1 沿用固定版本，MySQL／SQLite 的中立資料存取補正也必須回歸。Windows 使用非 login shell；工具位置與 sandbox 權限分別處理。

先盤點 Docker project、container、network、volume、ports 與資源，確認 15542、15543、14322、18180 以及生成物使用的 15642、18280、18281 沒有被其他工作占用。下列名稱僅限本次測試；不得接管已存在的同名 project。

```powershell
./scripts/build-test-idp.ps1
$env:PANGOLIN_SOURCE_COMMIT = (git rev-parse HEAD).Trim()
docker compose -p pangolin-m6-dev -f deploy/compose/m1.yaml -f deploy/compose/m5-databases.yaml up -d --build --wait --wait-timeout 120

$env:PANGOLIN_M1_ADMIN_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_APP_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_WORKER_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_NATS_URL = 'nats://127.0.0.1:14322'
$env:PANGOLIN_M1_ISSUER = 'http://127.0.0.1:18180'

python scripts/m6.py verify --project pangolin-m6-dev
python scripts/m6_linux.py --project pangolin-m6-dev
python scripts/m6.py gate --commit <完整來源 SHA>
```

以上密碼只供隔離 fixture。Windows sqlc 使用 `.cache/bin/sqlc.exe`，固定 Linux runner 使用 `.cache/linux-bin/sqlc`。M6 Linux 入口從固定 Go／PostgreSQL digest 建置 `<project>-runner`，內建 18.7 備份工具，不取得 Docker socket；Broker 故障操作由驗證 project ownership 的 host barrier 負責。Gate 在累積 integration／race 之前選擇相容的主機 pg clients，或透過該隔離 project 的 PostgreSQL 容器執行；較舊的主機 client 不會被誤用。可透過 `PANGOLIN_M6_PG_*` 明示工具來源，詳見 [發布演練](../runbooks/release.md)。

## 驗收與證據

M6 必須保留 M0–M5 累積回歸與資料庫補正，執行 A01–A95 的 421 個具名情境，核對 tenant-only、organization-enabled、replacement-contract 的適用性。必要案例失敗、缺失或 skip，以及任一關鍵 package 的原始 coverage 計數不足 90.00%，均阻擋發布；不能以平均百分比或另一 Profile 的成功掩蓋。

另外保存乾淨重建、前向升級、實際備份／還原與獨立連線對帳、含 p95／p99 的效能基線、安全審查及版本一致的發布清單。每份證據綁定來源 commit、來源 bytes、固定依賴、Profile、seed、命令、結果與原始檔案 hash。效能數字描述該硬體與負載，不構成生產容量承諾。

正式執行前先完成 review 並保存乾淨來源；測試期間凍結來源。dirty run 保留供除錯，但不能轉成 Verified。若需要修程式，建立新來源並重新收集全部證據。發布驗收不等於建立 tag、推送遠端、部署或公開發布。

## 文件及清理

模組／production package README 與程式一起維護。M6 Runtime Gate 通過後，才更新 Docusaurus 使用者教學，實際執行 consumer 範例、站點測試、production build 與初學者走查。公開導覽不放內部里程碑或 Gate 記錄；正式發行前不建立版本快照。

保存專用服務 logs 後，確認資源 labels 與名稱皆屬於本次 project，再清除 containers、network、volumes、專用測試 image 與 Linux cache；保留 workspace 的驗證 logs。不操作其他 project 或原有 docs 服務，也不使用全域 prune。

```powershell
docker compose -p pangolin-m6-dev -f deploy/compose/m1.yaml -f deploy/compose/m5-databases.yaml logs --no-color > artifacts/m6-compose.log
docker compose -p pangolin-m6-dev -f deploy/compose/m1.yaml -f deploy/compose/m5-databases.yaml down --volumes --rmi local
docker volume inspect pangolin-m6-dev-linux-cache
docker volume rm pangolin-m6-dev-linux-cache
# 確認沒有 container 使用，且 image 的 owner/purpose labels 屬於本次 M6：
docker image inspect pangolin-m6-dev-runner
docker image rm pangolin-m6-dev-runner
```
