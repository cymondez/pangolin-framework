<p align="center"><img src="docs/assets/pangolin-banner.jpg" alt="panGOlin — Go Modular Application Framework" width="100%"></p>

# panGOlin-framework

Go 模組化應用框架，採模組化單體、明確 DI、UOW、Tenant／Scope 隔離與可靠事件。產品目標 v0.1.0，規範基線 v1.3。

M1–M6 同屬 [VERSION](VERSION) 指定的 **0.1.0 開發週期**，里程碑推進不改套件版本；M6 發布候選已通過同版 Gate，尚未建立正式發行 tag。版本對齊及獨立版本領域見 [ADR11](docs/adr/ADR11-product-version.md)。

**M0／M1／M2／M3／M4／M5 已通過 Gate。** M1 分支為 `codex/m1-vertical-flow`，同版 Windows／Linux（含 race）證據見 [M1 驗收紀錄](docs/evidence/M1.md)。 新增 Kernel／DI／Config／Drain 與 tenant-only Bookstore 真實交易及分程序事件薄片，見 [M1 開發與驗證](docs/guides/m1-development.md)、[API 增量](docs/release-notes/M1.md)。M2 來源 `23651f65` 已通過同版 Windows／Linux（含 race）Gate，見 [M2 驗收紀錄](docs/evidence/M2.md)，開發分支為 `codex/m2-unit-of-work`，操作入口為 [M2 指南](docs/guides/m2-development.md)；M3 來源 `7f17ecc7` 已通過同版 Windows／Linux（含 race）Gate，完成安全／治理、通用可靠事件、遷移與冪等，見 [驗收紀錄](docs/evidence/M3.md)及 [M3 指南](docs/guides/m3-development.md)；M4 來源 `43dcd79e` 已通過同版 Windows／Linux（含 race）Gate，交付持久化 Jobs、容量背壓、觀測與活躍停機，見 [M4 驗收](docs/evidence/M4.md)及 [M4 指南](docs/guides/m4-development.md)；M5 來源 `88f32eb6` 已通過 Cache／Settings／Features、CLI 與兩種生成 Profile 的同版 Windows／Linux（含 race）Gate，分支為 `codex/m5-cli-platform-services`，見 [M5 驗收](docs/evidence/M5.md)及 [M5 指南](docs/guides/m5-development.md)。

M5 後續修正來源 `0d811a21f8b599196f0bad2dd2ee9033d1aa184b` 已通過同版 Windows／Linux（含完整 race）累積 Gate：Settings 五至七層、Feature 受 Permission 限制、PostgreSQL／MySQL／SQLite 中立資料存取及可擴充登入，見[修正驗收](docs/evidence/M5-corrections.md)。Framework 版本保持 0.1.0；完整 Profile 的引擎專用 Adapter 與未來登入協定邊界見各模組說明。

**M6 已 Verified**，來源 `1d7ef1cf0dfdb7ee7b0211d04ad3380329883cb1`，分支 `codex/m6-release-acceptance`。95 項需求、421 個具名情境、三組 Profile、重建／升級／備份還原／安全／效能演練及累積品質通過同版 Windows／Linux Gate，Linux 含完整 integration race；證據見 [M6 驗收紀錄](docs/evidence/M6.md)。Docusaurus 已於驗收後更新，操作與走查見[網站 README](docs/site/README.md)及[初學者檢視](docs/guides/docs-beginner-review.md)。

## 本機檢查

先看 [倉庫目錄與程式歸屬](docs/guides/repository-layout.md)、[Bookstore 入口](examples/bookstore/README.md)、[測試索引](tests/README.md) 與 [工具索引](tools/README.md)。Bookstore 專用 Handler／main 已集中到範例；搬移相容性見 [目錄整理說明](docs/release-notes/source-layout.md)。

各模組的使用、設定、生命週期、測試與交付邊界：

| 領域 | 模組說明 |
|---|---|
| Kernel | [Module](kernel/module/README.md)、[DI](kernel/di/README.md)、[Config](kernel/config/README.md)、[Runtime](kernel/runtime/README.md) |
| Persistence | [中立資料存取](persistence/database/README.md)、[UOW](persistence/uow/README.md)、[PostgreSQL](persistence/postgres/README.md)、[MySQL](persistence/mysql/README.md)、[SQLite](persistence/sqlite/README.md)、[Migration](persistence/migration/README.md)、[Idempotency](persistence/idempotency/README.md) |
| Security | [Identity／Authentication Port](security/identity/README.md)、[JWT／服務 Adapter](security/authn/README.md)、[Scope](security/scope/README.md)、[Permission](security/policy/README.md)、[Tenant](tenancy/README.md)、[Audit](platform/audit/README.md) |
| Events | [Envelope／Codec](events/distributed/README.md)、[NATS JetStream](events/nats/README.md)、[Local](events/local/README.md)、[Outbox](events/outbox/README.md)、[Inbox](events/inbox/README.md)、[PostgreSQL](events/postgres/README.md) |
| HTTP／範例 | [HTTP](examples/bookstore/transport/http/README.md)、[Bookstore 分層與組裝](examples/bookstore/README.md) |
| Application／Transport | [Command／Query](application/README.md)、[HTTP／串流](transport/http/README.md) |
| 可選組織 | [單一組織模組](modules/organization/README.md)、[管理 Port](modules/organization/contracts/README.md) |
| 平台設定／CLI | [Cache](platform/cache/README.md)、[Settings](platform/settings/README.md)、[Features](platform/features/README.md)、[pangolin](cmd/pangolin/README.md) |
| 工程 Gate | [品質計數](internal/quality/README.md)、[check 命令](tools/check/README.md)、[M6 重現指南](docs/guides/m6-development.md) |

使用 Go 1.27.4、Python 3.11+、sqlc v1.31.1 及真實 PostgreSQL／NATS／測試 IdP，並回歸 MySQL／SQLite 資料存取。先依 [M6 指南](docs/guides/m6-development.md) 啟動隔離環境與設定連線，再執行 Windows PowerShell：

```powershell
./scripts/check.ps1 -Project pangolin-m6-dev
```

Linux（race 需要 C compiler／CGO）：

```sh
PANGOLIN_M6_PROJECT=pangolin-m6-dev sh scripts/check.sh
```

`python scripts/m0.py check` 另檢查公開 API surface、421 個具名驗收計畫、三種 Profile 的適用性、fixture、import 邊界與 固定依賴／license allowlist。API 變更須先同步規範／ADR，再明示執行 `python scripts/m0.py freeze-api`；CI 不自動更新快照來放過漂移。

正式 M0 證據先固定為乾淨 commit，再執行 `python scripts/m0.py verify`；固定 Linux runner 加 `--race`，本專案空環境加 `--environment --project <name>`。三組結果保存於 `artifacts/<commit>/m0/`，最後以 `python scripts/m0.py gate --commit <full-sha>` 驗證相同版本、log hash、必要檢查與 Runtime 發布拒絕防護。這只判定 M0，不驗收後續 Runtime 功能。

現行累積檢查包含 build／vet、全部案例、真實 Integration／插樁 E2E、整體及每個關鍵 package 的 90% statement coverage，以及 Linux race。`internal/testkit` 與可重現的 sqlc 生成物逐檔列在排除 manifest；必跑案例 skip 等同失敗。`go run ./tools/check -release` 以非零結束，拒絕僅以靜態 inventory 認證發布；正式發布證據使用 `python scripts/m6.py gate --commit <完整來源 SHA>` 核對雙平台、逐情境索引、演練與原始檔案 hash。

## 隔離的測試環境

詳見 [M0 環境指南](docs/guides/m0-environment.md)。操作前先盤點既有環境。預設 Compose project 為 `pangolin-m0`，不占用本機 5432／4222；服務皆綁 loopback，資源總上限 512 MiB／2 CPU。不要重啟共用 Docker Desktop 來處理本專案連線問題。

```powershell
./scripts/build-test-idp.ps1
docker compose -p pangolin-m0 -f deploy/compose/compose.yaml config --quiet
docker compose -p pangolin-m0 -f deploy/compose/compose.yaml up -d --build --wait --wait-timeout 90
python scripts/smoke.py
```

這些命令是環境驗證流程，需先確認名稱／port 沒有衝突。`smoke.py` 不啟停任何容器，只操作已確認屬於該專案的 DB 與測試 Broker／proxy，並還原自己的故障開關。Test IdP 是無驗證的本機 fixture，不能作正式身分供應商。

## 文件入口

- [官方文件網站：本機預覽、維護與 Docker 部署](docs/site/README.md)
- [套件使用範例：獨立 Go 專案](examples/basics/README.md)
- [代理導覽與共用環境操作規則](AGENTS.md)
- [實作規範](design/panGOlin-framework_實作規範書.md)
- [里程碑、進度與估算](design/panGOlin-framework_里程碑規劃書.md)
- [M0 ADR](docs/adr/README.md)
- [驗收底稿](tests/contract/README.md)

GitHub Actions 已配置完整 Linux M6 Gate 與 Windows portability tests；完整跨平台發布 Gate 另要求 Windows 真實服務 report。本次驗收在本機雙平台完成，實際 push／執行後才有遠端 workflow 證據；正式 tag 發行流程尚未執行。

## 設計靈感與致謝（Acknowledgments）

panGOlin Framework 是一套以 Go 語言獨立設計並從零實作（clean-room implementation）的專案，未複製、移植或衍生自任何外部框架的原始碼。

在模組化單體架構分層、工作單元（Unit of Work）、多租戶與資源 Scope 隔離、分層設定與功能旗標、以及文件導覽組織等高層架構概念上，本專案受到開源社群與 [ABP Framework](https://abp.io/) 等現代企業級應用框架之設計理念啟發，謹此致謝。

> **獨立聲明**：panGOlin Framework 與 ABP Framework（VoloSoft）及上述開源專案之間無任何官方隸屬、合作、授權或背書關係；相關名稱僅作設計理念參考致謝之用，其商標與權利歸屬各自原權利人。

## 授權條款（License）

本專案採用 **Apache License, Version 2.0** 授權，完整條文與歸屬聲明見 [LICENSE](LICENSE) 與 [NOTICE](NOTICE)。透過 `pangolin` CLI 生成的使用者專案業務程式碼歸使用者自行選擇授權，內附的 `framework/` Runtime 目錄則保留框架本身的 `LICENSE` 與 `NOTICE`。
