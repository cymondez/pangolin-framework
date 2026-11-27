# 工程品質檢查

此內部工具檢查需求／案例／交付映射、production source inventory，以及 atomic coverage block 聯集；不自行證明交易或 Broker 語意。入口為 [tools/check](../../tools/check/README.md)；完整累積驗收由 `scripts/verify.py` 重用 `scripts/test_runner.py` 收集。靜態案例計畫保持 planned，`release=true` 拒絕以盤點代替執行證據，錯誤會指向同 commit 的發布 Gate。

SourceInventory 以實際 `go list -json` 的檔案為分母，未測 package 不能消失。排除只接受逐檔 test fixture，或具有正確 sqlc 標記／版本且可重現的生成物。Counts.Pass 以原始整數 `covered*100 >= total*90` 比較，零分母不偽記 100%。CountCoverage 拒絕缺 profile、未知來源與不一致 statement metadata。

驗證：`go test ./internal/quality`；fixtures 涵蓋 89.99%／90.00%、未測 package、重複 block、缺 instrumentation 與非法排除。
