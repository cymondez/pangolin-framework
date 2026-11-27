# 開發工具入口

所有命令都從 repository root 執行。Go 檢查命令在 `tools`，跨平台操作入口與 Python 支援程式在 `scripts`；測試資料及 fixture 依 [測試索引](../tests/README.md) 歸類。

| 工作 | 入口 |
|---|---|
| Windows 完整累積驗證 Gate | `./scripts/check.ps1 -Project <隔離名稱> [-Race] [-Scope release\|realtime]` |
| Linux 完整累積驗證 Gate，含 race | `PANGOLIN_PROJECT=<隔離名稱> sh scripts/check.sh` |
| 固定 Linux OCI `-race` runner | `python scripts/linux_runner.py --project <隔離名稱> [--target release\|realtime]` |
| 契約／API／import／依賴盤點 | `python scripts/contracts.py check` |
| 凍結已審查 API 變更 | `python scripts/contracts.py freeze-api`；不在 CI 自動執行 |
| Go 契約／coverage 計數 | [check](check/README.md)：`go run ./tools/check` |
| 同 commit 跨平台發布 Gate | `python scripts/verify.py gate --commit <full-sha> [--scope release\|realtime]` |
| 測試 IdP 建置、環境 smoke、生成計畫 | [scripts 說明](../scripts/README.md) |

完整 Gate 需要固定版本服務、連線變數與 sqlc，見 [M6 指南](../docs/guides/m6-development.md) 與 [M7 指南](../docs/guides/m7-development.md)。`check.ps1`／`check.sh` 是維護者的統一驗證入口；低階支援程式不另包成產品 CLI。生成物與 report 寫入 `artifacts`／`.cache`，不加入手寫 source 目錄。工具不把模組可編譯、文件盤點或單一案例成功當作 Runtime 驗收。
