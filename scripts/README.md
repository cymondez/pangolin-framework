# 操作腳本

目前完整入口為 `m6.py verify`／`m6_linux.py`，同版雙平台發布 Gate 為 `m6.py gate --commit <SHA>`；包含 M0–M5 累積回歸、三引擎補正與兩種生成 Profile。M6 另收集五項發布演練、版本與交付清單、逐 Profile 子情境證據索引。詳見 [M6 指南](../docs/guides/m6-development.md)。歷史入口保留各自的驗收責任。

從 repository root 執行；Windows shell 使用 login=false。完整測試入口為 check.ps1／check.sh，環境與必要變數見 [M1 指南](../docs/guides/m1-development.md)。此目錄放操作腳本，Go 程式碼與測試 fixture 仍歸其擁有者。

| 腳本 | 用途 |
|---|---|
| `m6.py`／`m6_linux.py` | 全量累積驗收與同來源雙平台發布 Gate；Linux 使用固定 Go／PostgreSQL client 的專用 runner，必跑完整 race |
| `m6_drills.py` | 真實乾淨重建、前向升級、pg_dump／restore、延遲基線及可執行安全審查；失敗與缺漏證據均拒絕 |
| `release_manifest.py` | 95 項需求／421 情境閉合、交付物、Runtime／CLI／Template／Event schema 版本及 raw-count coverage 驗證 |
| `m6_index.py` | 將三種 Profile 的適用子情境連到實際 logs／hash；共用契約證據明示共用，不冒稱獨立執行 |
| `check.ps1`／`check.sh` | 完整累積 Gate；Windows 可明示 Race，Linux 必跑 race |
| `m1.py` | M1 Unit／Integration／E2E／coverage／生成檢查與同 commit Gate |
| `m1_e2e.py` | Gate 內部支援；建置並啟動範例自己的 API／Worker／故障程序 |
| `m0.py` | 現行契約／API／import／依賴盤點，以及綁定舊來源的 M0 evidence 流程 |
| `dependencies.py` | 完整 module／checksum／license allowlist 檢查，由盤點呼叫 |
| `product_version.py` | 由 `m0.py check` 檢查 VERSION、現行自有 Module 與基線 fixture 均為 0.1.0；不改第三方或 schema 版本 |
| `docs_examples.py` | 在獨立 Go consumer 專案執行公開教學的完整程式、環境覆寫與文件程式片段 |
| `build-test-idp.ps1` | 以固定 Go 工具鏈產生 Linux 測試 IdP；不啟動 Docker |
| `smoke.py` | 已確認歸屬的 M0 服務 smoke／故障裝置；不代替正式 Runtime 驗收 |
| `contract_matrix.py` | 421 個具名驗收計畫與 Profile manifest 的生成來源 |

Docker 啟停前先盤點專案／ports／volumes／資源，只操作本次隔離名稱；不重啟 Engine 或全域 prune。歷史腳本／命令的相容性以當時來源 commit 為準，現行完整 Gate 見 [工具索引](../tools/README.md)。

M2 現行累積入口為 m2.py，重用 m1.py 的工程檢查及分程序 E2E，再合併 M2 真實驗收。連線與清理見 [M2 指南](../docs/guides/m2-development.md)。

M3 完整入口為 `m3.py verify --project pangolin-m3-dev`；`m3_fixture.py` 提供 host-owned NATS 重啟 barrier，`m3_linux.py`／`m3_linux_entry.py` 在固定 OCI runner 複製相同原始 bytes 並跑 race；`m3.py gate --commit <SHA>` 核對雙平台同版證據。Windows check.ps1／Linux check.sh 目前指向 M3 累積 Gate，M1/M2 歷史入口保留。

M5 新增 m5_templates.py（兩種生成物的真實 SQL／HTTP／UOW）、m5_compose.py（host 上的兩種生成 Compose 並清理）、template_bundle.py（同版 Runtime 再生成及 drift Gate）、template_openapi.py（完整 Profile route/schema 文件）。入口、版本與證據限制見 [M5 指南](../docs/guides/m5-development.md)。
