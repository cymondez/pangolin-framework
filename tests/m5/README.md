# M5 累積驗收

A65／A80 平台行為以 platform/cache、settings、features 的具名測試及 tests/m3/platform_test.go 的真實治理 Authority／Audit 共同驗證。CLI 的 new／module／migration／help／conflict／重入在 internal/scaffold；本目錄驗證真實 Goose status。

scripts/templates_verify.py 在乾淨目錄生成兩種完整 Bookstore，以同版 bundled Runtime 建置、測試、套用部署 migration 並啟動 API；跑既有 UOW 安全契約、兩租戶／兩 Org 的同 key 隔離、idempotency replay/conflict、嚴格 DTO／hint／未驗證入口與控制面。資料庫由僅允許 m5_template_* 的 testkit 建立／清除，log 留在 artifacts。

完整入口 `scripts/verify.py`、`scripts/linux_runner.py`；同 commit 的 Windows／Linux（含 race）及累積回歸都必須成功。品質工具反例不能代替生成物執行。module README、版本／模板 drift、OpenAPI 完整 route/schema contract 與 references 都由 Gate 檢查。

A65 的必跑 regression_tests 加入真實 JSON → Configuration → Settings Resolve，含雙位數／巢狀 array、數字 property、點號 literal、null／空容器、型別邊界、覆寫／Scope 繼承／Cache／Update；既有正式案例 ID 保持不變。
