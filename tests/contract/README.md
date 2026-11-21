# 符合性驗收底稿

`cases.json` 保存實作規範 v1.3 的 A01–A95、需求 ID、唯一交付 milestone；目前 Runtime 狀態全部 planned。`subtests.json` 已依 §13.6 拆成 421 個具名子情境，引用唯一規範列的 setup／刺激／預期與 fingerprint。`profiles/` 逐項記錄三種組裝的適用性、理由、狀態與 evidence 路徑；實際 Runtime 測試函式依 M1–M6 交付。

`go run ./tools/check` 檢查規範定義、驗收映射、里程碑映射及 manifest 的一致性，不執行 95 項 Runtime 驗收。`-release` 必須失敗；不允許將 manifest 手改成 verified 來通過。

`python scripts/m0.py check` 驗證凍結 API、subtest／Profile／fixture 的完整性及 import 邊界；`tests/m0/test_gate.py` 故意注入缺漏、錯誤 N/A、API 漂移、跨 commit 或 log 篡改等反例。它們是 M0 工程檢查，不掛名為已完成的 A01–A95 測試。

tenant-only 的組織成功子情境可依 §13.4 標明 N/A 理由；未知 scope hint、缺 Provider、非法降級等負測試仍適用。不將整個 ORG 群組直接 skip。

M1 的第一個真實切片：HTTP → CreateBook → Required Tx → PostgreSQL，保存提交／回滾／2xx 時序；第二個切片再接 JWT、RLS、Outbox／Broker／Inbox。完整案例驗收、CI gate 與正式交付責任依里程碑維護。

## Coverage 計數 fixture

`internal/quality` 的測試驗證 89.99%／90.00%、未測 package、重複 statement block 聯集、缺 instrumentation／profile 及不一致 metadata。CLI 暫時只接收同一 build 的一份 atomic profile；Unit／Integration／Contract／E2E 的同 build metadata 合併與證據 collector 尚待 M1。不得將不同 build 的 profile 直接合併。

`tests/coverage/exclusions.json` 逐檔排除測試 IdP fixture。手寫品質工具仍在分母；尚未有 executable statements 的根 package 為 N/A，不回報 100%。這不是全框架已達 90% 的證據。
