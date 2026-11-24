# M5 開發與驗收

M5 原始交付於來源 `88f32eb6f2aa02c601dbc930f3f5a7b76911e185` 通過雙平台 Gate，見 [驗收紀錄](../evidence/M5.md)。後續 Settings 標準分層、中立資料存取及可擴充 Authentication 已於來源 `0d811a21f8b599196f0bad2dd2ee9033d1aa184b` 通過同版 Gate，見 [修正驗收](../evidence/M5-corrections.md)。以下為目前驗收入口。

開發分支 `codex/m5-settings-multidatabase`，Framework 版本固定 0.1.0。正式責任 PLAT-001／CLI-001／SCP-008／ORG-013；25 個具名情境見 tests/contract/m5.json。工作樹測試不代替同 commit 的雙平台 Gate。

先依 [M3 指南](m3-development.md) 盤點 Docker／ports／volumes，使用 `pangolin-m5-dev` 專用環境。Go 1.27.4、PostgreSQL 18.7、NATS 2.15.3、Goose 3.30.0、sqlc 1.31.1 沿用既有固定版本；連線變數沿用 PANGOLIN_M1_*，不可操作其他專案或使用者的 docs 服務。

```powershell
./scripts/build-test-idp.ps1
docker compose -p pangolin-m5-dev -f deploy/compose/m1.yaml -f deploy/compose/m5-databases.yaml up -d --build --wait --wait-timeout 120
python scripts/m5.py verify --project pangolin-m5-dev
python scripts/m5_linux.py --project pangolin-m5-dev
python scripts/m5.py gate --commit <完整來源 SHA>
```

Windows 預設使用 `.cache/bin/sqlc.exe`，固定 OCI runner 需要 `.cache/linux-bin/sqlc`；也可用 PANGOLIN_SQLC 指定目前平台的固定版本工具。每次正式驗收使用全新輸出目錄，先保留既有結果再重跑，避免重用已生成的專案。

三引擎契約使用同一個 `persistence/database` Port。SQLite 使用實際 Driver；PostgreSQL／MySQL 使用專用 Compose 服務，額外映射列在 tests/contract/m5.json 的 regression_integration_tests。`PANGOLIN_DATABASE_POSTGRES_DSN`、`PANGOLIN_DATABASE_MYSQL_DSN` 可覆寫測試連線；未指定時使用上述隔離 fixture。業務 Repository／Application 依賴中立 Port 或自己的 Repository 介面，Driver 選擇只留在組裝與 Adapter。既有依賴引擎 RLS 的完整 Profile 不得悄悄換成沒有該能力的引擎。

先執行 development run，修正後以乾淨 commit 收集 Windows／固定 OCI Linux 同一 source bytes 的證據。Linux 必跑 race。完整 Gate 包含 M0–M4 累積反例、真實 SQL／Broker／分程序 E2E、sqlc 再生成、依賴／license、CLI 子程序插樁 coverage，整體與各關鍵 package 分別以原始計數比較 90.00%。

M5 增加 Memory Cache／分層 Settings／Features、現行父層 Authority 與 Audit owner 回歸。CLI 所有命令的 help、錯誤退出、重入／檔案保護、OpenAPI route/schema／reference 都驗證。`scripts/template_bundle.py --check` 比對模板內 Runtime 與同版 production source，不能使用舊 bundle 掩蓋漂移。

生成物在乾淨 runner 建置兩 Profile，直接 go test，實際套用 migrations／seed／使用者新建 migration、啟動 API、執行同 Runtime 的 UOW／RLS／JWT／完整 Scope／冪等及治理控制面。模板自帶同版 runtime，v0.1.0 尚未發行時不依賴遠端不存在的 tag。固定種子資料僅限 disposable fixture。

Windows Gate 另用生成出的 Compose 執行兩 Profile，project 名稱為 `<M5-project>-template-{tenant,organization}`，依序使用 loopback 15642／18280／18281。執行前拒絕已存在的同名 project；完成後移除自己建立的 containers／network／volumes／local images。Linux CI 可設定 PANGOLIN_M5_COMPOSE=1 執行同一流程；無 Docker socket 的固定 OCI runner只執行原生生成物，Compose 證據由同版 host Gate 提供。

命令／HTTP／reconciliation／生成物／coverage 與原始 logs 留在 `artifacts/<commit>/m5/{windows-amd64,linux-amd64}`，dirty run 另存 development 路徑。完整 Gate 通過前保持 In progress／Evidence ready，不標 Verified；Docusaurus 使用者內容在驗收通過後更新。

完成後確認 ownership，再清除專用服務與 runner cache：

```powershell
docker compose -p pangolin-m5-dev -f deploy/compose/m1.yaml -f deploy/compose/m5-databases.yaml down --volumes --rmi local
docker volume inspect pangolin-m5-dev-linux-cache
docker volume rm pangolin-m5-dev-linux-cache
```

不使用全域 prune，保留 workspace evidence／logs。CLI API、支援邊界與升級見 [變更說明](../release-notes/m5.md)，各 package 的組裝／錯誤／清理責任見同目錄 README。
