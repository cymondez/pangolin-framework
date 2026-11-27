# M4 開發與驗收

分支 `codex/m4-platform-services`，Framework 版本仍為 0.1.0。交付責任是 OPS-002、JOB-001、OBS-001；CLI 與 Settings／Features 屬於 M5。

先依 [M3 開發指南](m3-development.md) 盤點 Docker／ports，建立 `pangolin-m4-dev` 專用環境，保留既有 docs 與其他專案。固定 Go 1.27.4、PostgreSQL 18.7、NATS 2.15.3、Goose 3.30.0、sqlc 1.31.1。連線與工具變數沿用 M3 指南，專用 Compose 命令改用：

```powershell
./scripts/build-test-idp.ps1
docker compose -p pangolin-m4-dev -f deploy/compose/m1.yaml up -d --build --wait --wait-timeout 60
python scripts/m4.py verify --project pangolin-m4-dev
python scripts/m4_linux.py --project pangolin-m4-dev
python scripts/m4.py gate --commit <完整來源 SHA>
```

先執行 dirty development run 修正問題，再以乾淨 commit 收集雙平台同版證據。Linux runner 使用固定 Go image，複製完全相同的 source bytes 並執行 race。完整 Gate 包含 M0–M4 工程反例、真實整合、分程序 E2E、sqlc 再生成、依賴／license 與原始 statement block 聯集；適用案例 skip 失敗。

M4 另外保存 `operations.json` 的 64 producer／10,000 Job 負載曲線、A63 Broker 恢復對帳與正常／逾時 drain logs。測得的 RTO 與記憶體／連線數只適用固定 fixture，不視為正式容量 SLA 或生產 RPO 保證。完整操作流程見 [運維手冊](../runbooks/operations.md)。

完成後保留 workspace artifacts／logs，核對 ownership 後 `docker compose -p pangolin-m4-dev -f deploy/compose/m1.yaml down --volumes --rmi local`；檢查並移除 `pangolin-m4-dev-linux-cache` 專用 volume。不得使用全域 prune。供使用者瀏覽的 `pangolin-docs` 保持運行。
