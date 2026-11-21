# 操作腳本

從 repository root 執行；Windows shell 使用 login=false。完整測試入口為 check.ps1／check.sh，環境與必要變數見 [M1 指南](../docs/guides/m1-development.md)。此目錄放操作腳本，Go 程式碼與測試 fixture 仍歸其擁有者。

| 腳本 | 用途 |
|---|---|
| `check.ps1`／`check.sh` | 完整累積 Gate；Windows 可明示 Race，Linux 必跑 race |
| `m1.py` | M1 Unit／Integration／E2E／coverage／生成檢查與同 commit Gate |
| `m1_e2e.py` | Gate 內部支援；建置並啟動範例自己的 API／Worker／故障程序 |
| `m0.py` | 現行契約／API／import／依賴盤點，以及綁定舊來源的 M0 evidence 流程 |
| `dependencies.py` | 完整 module／checksum／license allowlist 檢查，由盤點呼叫 |
| `build-test-idp.ps1` | 以固定 Go 工具鏈產生 Linux 測試 IdP；不啟動 Docker |
| `smoke.py` | 已確認歸屬的 M0 服務 smoke／故障裝置；不代替正式 Runtime 驗收 |
| `contract_matrix.py` | 421 個具名驗收計畫與 Profile manifest 的生成來源 |

Docker 啟停前先盤點專案／ports／volumes／資源，只操作本次隔離名稱；不重啟 Engine 或全域 prune。歷史腳本／命令的相容性以當時來源 commit 為準，現行完整 Gate 見 [工具索引](../tools/README.md)。
