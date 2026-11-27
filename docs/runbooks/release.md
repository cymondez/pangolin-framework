# 發布演練操作手冊（Release Drills Runbook）

本手冊定義 v0.1.0 發布前必須實際執行的發布演練（A72／REL-005）：乾淨重建、前向升級、備份／還原、效能基線與安全審查。可執行入口是 `scripts/release_drills.py`；演練不啟停任何服務，只使用既有的固定版本 fixture（PostgreSQL 18.7、NATS 2.15.3、測試 IdP），並對應 [M6 開發指南](../guides/m6-development.md) 的環境。操作邊界與清理責任見 [operations runbook](operations.md)；本檔不重複佇列訊號處置。

## 執行入口與前置條件

```powershell
# 服務已由既有 fixture project 啟動（同 M6 驗收環境），並設定：
$env:PANGOLIN_M1_ADMIN_DSN   = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_APP_DSN     = 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_WORKER_DSN  = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_NATS_URL    = 'nats://127.0.0.1:14322'
$env:PANGOLIN_M1_ISSUER      = 'http://127.0.0.1:18180'

python scripts/release_drills.py --output artifacts/m6-drills-<label> --project pangolin-m6-<suffix>
```

- `--project` 必須是專屬的 `pangolin-m6-*` 名稱；release gate 傳入已盤點並運行的 verify project，作為識別與 Docker pg client fallback 的依據。獨立執行時可用 `--compose-project`／`PANGOLIN_M6_COMPOSE_PROJECT` 明示服務 project。
- 輸出目錄必須不含既有的 `release-drills.json`；所有命令 log、證據檔與 dump 都寫在該目錄並以 SHA-256 綁進報告。
- 需要的環境變數缺一即失敗（fail-closed）；`release-drills.json` 的 `status` 只有五項演練全部通過才是 `passed`，腳本 exit 0。

## 五項演練的實際內容

1. **clean-rebuild**：把目前工作樹的來源 bytes 匯出到 `<output>/clean-source`，以全新 `GOCACHE`／`GOTMPDIR` 執行 `go build ./...`（無任何既有建置產物），再於匯出樹內執行 `TestM6CleanRebuild`：建立唯一的新資料庫（`m6_rebuild_*`）、以真實 migration set 部署、建置 Bookstore API／Worker 二進位、以測試 IdP 的 RS256 JWT 完成 HTTP 寫入／讀取、驗證 2xx 之前交易已提交（新連線對帳 books＋pending Outbox）、偽造 Token 與 `X-Tenant-ID` 偽造分別回 401／403、Worker 將事件經 JetStream 送到 Inbox 並投影 Read Model（獨立連線對帳 `sent:1`、`processed`）。演練結束刪除匯出樹與建置快取，資料庫由測試 cleanup 保證刪除；JetStream stream 由測試建立並刪除。
2. **forward-upgrade**：以真實 M1 tenant-only fixture schema 建立「前版 schema」，於 app/worker 角色的 transaction-local RLS session 內種子兩租戶各 30 筆 books（含軟刪除）、book_views、Outbox 與 Inbox 資料，再用部署用 migration runner 一次套用所有待套用遷移（tenant、audit、scope、events、idempotency、jobs、capacity、organization 與真實 bookstore scope upgrade）。斷言：owner 全數保留為 `tenant/TenantID` 且不猜測 Organization、資料投影與未動表 checksum 完全一致、`pangolin.books` 已改名且舊 API 相容性探測失敗、`scope_books` 僅剩 `exact_owner` 政策（無寬鬆 policy union）、FORCE RLS 保持、升級後版本號與相容性檢查通過、app 角色跨租戶讀寫為零列且 org-owned 列可由新 schema 服務。
3. **backup-restore**：在唯一資料庫部署完整模組（含 organization）與 scoped bookstore schema，種子兩租戶各 25 筆 scope_books、8 筆 scope_outbox、10 筆 org-owned 書、治理資料（enterprise／organization／owner membership）。先以新連線記錄每張表的 count＋row-to-json MD5 checksum、RLS 旗標、政策與角色姿態，再執行真實 `pg_dump --format=custom`、建立新資料庫、`pg_restore`，然後**只用全新連線**對還原庫逐表比對 owner／keys／counts／checksums，並驗證 RLS／FORCE 旗標、政策集合、角色授權、app 角度讀寫與 migration 版本表全部還原成功。
4. **load-baseline**：在唯一天然 tenant-only 部署上執行確定負載：800 筆量測操作、8 並行、40 筆暖機，每筆都是真實框架寫入（pangolin_app 角色在 FORCE RLS 下的 ReadWrite UOW：參數化 INSERT 業務列＋可靠事件 Outbox 列）。保存全部原始 per-op 延遲樣本、pool 飽和取樣與 heap 變化；腳本獨立重算 p50/p95/p99 並與測試輸出比對，另以新連線對帳 books／outbox 行數。數字描述該硬體與該負載，不構成任何生產容量承諾；方法與誤差記在 `methodology` 欄。
5. **security-review**：解析 [安全審查文件](../security/release-review.md) 的機器可讀控制清單（每個規範 §14.1 類別的攻擊面／濫用路徑／控制／可執行測試／剩餘風險），逐一套用所列可執行測試（unit＋真實服務 integration，含 `tests/m6:TestM6SecurityDrill` 的資料庫控制演練：角色姿態、FORCE RLS 不變量、tenant/org IDOR、注入 round-trip），全部通過且無 skip；存在未解決高風險項即阻擋發布。

## RPO／RTO 量測方法

- **RPO**：本次使用完成種子寫入後停止寫入的資料集，以全表對帳量測 `rows_missing=0`。`rpo.window_seconds` 是種子寫入完成後取得的資料庫時間至啟動 `pg_dump` 的觀測間隔，並非生產 RPO，也不是精確的 snapshot 建立時間。快照後的新增寫入不包含在該份備份中；正式環境須按備份頻率、歸檔與持續寫入負載另行量測（§14.3）。
- **RTO**：從 `pg_restore` 開始到「以全新連線完成每張表一致性驗證」的 wall-clock（`rto.restore_seconds`＋`rto.verify_seconds`）。同樣是演練規模的實測值，隨資料量與硬體變化。

## pg client fallback 與 host-run 程序

backup-restore 需要真實 `pg_dump`／`pg_restore`，`scripts/release_drills.py` 的解析順序：

1. `--pg-bin`／`PANGOLIN_PG_BIN` 指定的主機 binary 目錄；
2. PATH 上的 `pg_dump`＋`pg_restore`（client major ≥ server major，否則失敗）；
3. Docker CLI 可用且 `<compose project>` 的 `postgres` 服務實際在運行時，透過 `docker compose -p <project> -f <file> exec -T postgres pg_dump/pg_restore`（容器內 client 版本恆與 server 一致；DSN 主機改寫為容器 loopback）；
4. 都不可用時演練 fail-closed：在具備工具的 host 上執行相同命令，並由 release gate 依 `PANGOLIN_M6_DRILLS_DIR` 明確驗證 host-run 證據的來源 provenance（同 commit、同 source bytes、dump hash 與 `release-drills.json` 內容）。固定 OCI Linux runner 內建 pg 18.7 client（見 `deploy/docker/Dockerfile.m6-runner`），不需要 Docker socket。

Go 測試本身（例如累積 Gate 直接跑 `go test -tags=integration ./tests/m6`）的解析順序相同：`PANGOLIN_M6_PG_MODE`／`PANGOLIN_M6_PG_BIN_DIR`／`PANGOLIN_M6_PG_CONTAINER_ARGV` 提示 → PATH → **`PANGOLIN_M6_COMPOSE_PROJECT`**（單一環境變數指名已盤點的 fixture project，測試自行組出 `docker compose ... exec` 命令；未指名的 project 一律拒絕）。在沒有主機 pg 工具的 Windows host 上跑完整 Gate 前，先設定例如 `$env:PANGOLIN_M6_COMPOSE_PROJECT = 'pangolin-m6-dev'`（可另以 `PANGOLIN_M6_COMPOSE_FILE` 覆寫 compose 檔，預設 `deploy/compose/m1.yaml`）。

host-run 步驟（僅在 runner 無工具且無 Docker 時）：

1. 在與驗收同 commit 的乾淨 host 上，以相同 `PANGOLIN_M1_*` 環境與專屬 project 執行 `python scripts/release_drills.py --output <workspace>/artifacts/m6-drills-host --project pangolin-m6-<suffix>`；
2. 將輸出目錄保留在 workspace `artifacts/` 內，於 Linux 驗收時設定 `PANGOLIN_M6_DRILLS_DIR` 指向該目錄；gate 會逐檔 hash 對帳、驗證 provenance 後原位複製採用。

## 證據綁定與清理

- 報告記錄 commit、`source_sha256`（與 `scripts/test_runner.py` 相同的工作樹 bytes 雜湊）、平台、project、每個命令的 argv／exit code／log hash，以及五項演練的 checks；`release_manifest.py` 對 schema 嚴格驗證（命令記錄僅含 `argv`/`exit_code`/`log`/`sha256`）。
- 演練只建立自己的資源：唯一資料庫（測試 cleanup 刪除，含失敗路徑）、一個 JetStream stream（測試刪除）、clean-source 匯出與建置快取（演練刪除）。不操作其他 project、不碰既有資料、不做全域 prune。
- dirty 工作樹可跑除錯，但報告標記 `development-only`；只有乾淨 commit 的演練證據可進入 release gate 的 Verified 判定。
