# M5 後續修正驗收

核對日期：2026-11-24。**Verified**，Runtime 來源 `0d811a21f8b599196f0bad2dd2ee9033d1aa184b`，分支 `codex/m5-settings-multidatabase`，產品版本 0.1.0、基線 v1.3。原始 M5 的 88f32eb6 證據保持其歷史來源；本紀錄驗證後續修正，文件 follow-up 不取代 Runtime 來源。

Windows／固定 OCI Linux 使用同一乾淨 commit 與來源 bytes，雙平台 `scripts/m5.py gate` 通過。25 個正式情境、56 項必要單元回歸映射、7 項跨 Backend 整合映射，以及 M0–M4 累積反例、分程序 E2E、兩種生成 Profile 均通過；Linux 另執行完整 integration race。

## 已驗證修正

- Settings：Default／Config／Global／Tenant／User 五層，可信 Enterprise／Organization 可選而成六或七層；Global 與 Config 獨立，完整 JSON flatmap、繼承、Scope 隔離、版本與 Cache 回歸通過。
- Feature 受 Permission 限制：先驗 Permission，再取功能值；普通 Permission 判斷不反向依賴 Feature，deny／權威故障不讀功能來源。
- 中立資料存取：同一 Resource／Executor Port 對接 PostgreSQL、MySQL、SQLite。真實 PostgreSQL／MySQL 服務及真實 SQLite Driver 驗證提交／回滾、owner、Required／RequiresNew、唯讀與生命週期；SQLite 另驗每個連線的外鍵與原生寫鎖限制。
- 清理：單一有界 cleanup、取消進行中的 SQL、晚註冊 owner、未釋放 Row／Rows、同時 Close／Err 與池回收；使用 channel barrier 控制競爭，race 通過。
- Identity：協定中立 Verifier／Pipeline 與私有身分 context；JWT／ServiceVerifier 相容轉接，自訂使用者 Session 經當前 Tenant／Scope／實際 policy.Require，明確 deny／撤權／權威故障拒絕。
- HTTP 與範本：可擴充憑證來源、重複／格式錯誤／多來源拒絕、Cookie unsafe method 拒絕；Challenge／Pending 不安裝身分。Bookstore、組織控制面與兩種生成 Profile 均完成接線。

## Coverage

| Runner | 手寫 production statements | 百分比 |
|---|---:|---:|
| windows-amd64 | 5393／5789 | 93.1594% |
| linux-amd64 | 5392／5789 | 93.1422% |

整體及每個關鍵 package 各以原始計數通過 90.00%，未平均百分比或排除新增 production code。完整計數、平台與命令摘要見[機器可讀索引](m5-0d811a21.json)。

Source SHA-256：`78b1baaa6cf412e7727b6e3da8d95f26764a67e26cd781dfd8a67de98a74998f`。原始 logs、profiles、生成物及對帳保留於 `artifacts/0d811a21f8b599196f0bad2dd2ee9033d1aa184b/m5/`；索引包含兩平台 report 與 Gate SHA-256。失敗的 296802e 執行因 TestBeginGuards 跳過而未列為通過；修正為明確拒絕斷言後，於本來源完整重跑。

## 交付邊界

MySQL／SQLite 交付中立資料存取與 UOW Adapter；既有完整 Bookstore、治理、Audit、可靠事件、Jobs 及 migration 仍有引擎專用實作，不能僅改 DSN 宣稱全部移植。缺少引擎 RLS／唯讀交易等必要能力時拒絕組裝。

Identity 提供未來 MFA／OIDC／Device Login 的擴充及未完成結果契約，沒有宣稱完整協定已實作。Cookie 寫入需要 Session 模組明示 CSRF 防護。M6 發布驗收、正式 tag 與遠端 CI 不在本紀錄的完成宣稱內。

Docusaurus 使用者內容在本 Runtime Gate 通過後更新，另以 consumer 範例、網站建置與初學者走查驗證。
