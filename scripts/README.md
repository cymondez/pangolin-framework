# 操作腳本

目前完整驗證與發布 Gate 的統一入口為 `scripts/verify.py`（`--scope release` 或 `--scope realtime`）與 `scripts/linux_runner.py`（固定 OCI Linux/amd64 `-race` runner）。同版雙平台發布 Gate 為 `python scripts/verify.py gate --commit <SHA> [--scope release|realtime]`。詳見 [M6 指南](../docs/guides/m6-development.md) 與 [M7 指南](../docs/guides/m7-development.md)。

所有腳本皆從 repository root 執行；Windows shell 使用 `login=false`。日常維護入口為 `scripts/check.ps1`（Windows）與 `scripts/check.sh`（Linux）。此目錄放操作與驗證腳本，Go 程式碼與測試 fixture 仍歸其擁有者。

| 腳本 | 用途 |
|---|---|
| `verify.py` | 統一累積驗證與同來源雙平台發布 Gate CLI（`verify`／`gate`，支援 `--scope release` 與 `--scope realtime`） |
| `linux_runner.py`／`linux_entry.py` | 固定 digest 的 Linux/amd64 OCI `-race` runner（`--target release\|realtime`），以逐位元相同來源執行驗證 |
| `check.ps1`／`check.sh` | 跨平台累積驗證包裝入口；Windows 可明示 `-Race` 與 `-Scope`，Linux 必跑 `-race` |
| `contracts.py` | 契約、凍結公開 API（`check`／`freeze-api`）、架構 import 邊界、設定上下界與依賴／版本盤點 |
| `test_runner.py` | Go/Python 測試 JSON 解析與驗證、atomic coverage block 聯集合併與累積測試執行引擎 |
| `e2e.py` | Bookstore 真實 API／Worker／crash-worker 分程序 E2E 與插樁 coverage 收集 |
| `nats_fixture.py` | Host 端隔離 NATS 重啟／啟停 file barrier 測試夾具 |
| `templates_verify.py` | CLI 生成專案（`tenant-only`、`organization-enabled`）的真實資料庫／HTTP／UOW／OpenAPI 與 Compose 驗證 |
| `release_drills.py` | 五項可執行發布演練（乾淨重建、前向升級、備份還原、延遲基線、安全審查）；失敗與缺漏證據均拒絕 |
| `release_manifest.py` | 95 項需求／532 情境閉合、交付物、Runtime／CLI／Template／Event schema 版本及原始計數 coverage 驗證 |
| `evidence_index.py` | 規範 §13.6 逐 Profile 子情境證據索引建構與驗證（支援 `release` 與 `realtime` 範圍） |
| `client_dependencies.py` | 官方 SignalR JS client（`@microsoft/signalr`）依賴與授權白名單檢查 |
| `dependencies.py` | Go 完整 module／checksum／license allowlist 檢查，由 `contracts.py check` 呼叫 |
| `product_version.py` | 由 `contracts.py check` 檢查 `VERSION`、自有 Module、CLI 與基線 fixture 均為 `0.1.0` |
| `template_bundle.py` | 同版 Runtime 與本地開發 IdP bundle 再生成與 `--check` drift Gate |
| `template_openapi.py` | 依模板路由與 schema 產生各 Profile 的 OpenAPI 3.1 規格 |
| `contract_matrix.py` | 具名驗收計畫與 Profile manifest 的生成來源 |
| `docs_examples.py` | 在獨立 Go consumer 專案執行公開教學的完整程式、環境覆寫與文件程式片段 |
| `build-test-idp.ps1` | 以固定 Go 工具鏈產生 Linux 測試 IdP；不啟動 Docker |
| `smoke.py` | 已確認歸屬的隔離 Compose 服務 smoke／故障裝置；不代替正式 Runtime 驗收 |

Docker 啟停前先盤點專案／ports／volumes／資源，只操作本次隔離名稱；不重啟 Engine 或全域 prune。現行完整工具索引見 [工具索引](../tools/README.md)。
