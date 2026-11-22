# M2 UOW／Persistence 與中立 Scope

目標：在 `codex/m2-unit-of-work` 完成里程碑 WP06–WP08、36 項正式需求與 M1 累積回歸；同版 Windows／Linux（含 race）Gate 通過後更新 Docusaurus。

權威：實作規範 v1.3 §1–3、§5、§7.6–7.10、§8、§13；里程碑 M2 與唯一需求映射。產品版本維持 0.1.0。

執行順序（單一代理）：
1. 建立 M2 案例映射與可重現 harness，先寫行為案例。
2. WP06：Options、Required／RequiresNew、deadline、受限視圖與資源識別。
3. WP07：Executor、Outcome／Unknown、清理／Panic／hook、Application 政策、HTTP／串流、版本競爭。
4. WP08：Scope Kind registry、組裝、單一 organization 邊界與兩 Org Bookstore 薄片。
5. 同步 package README、API 變更說明與維護指南，執行真實 PostgreSQL／Broker／故障驗證及累積 Gate。
6. 保存乾淨來源 commit 的雙平台證據。通過後更新 Docusaurus 使用文件並執行範例、網站檢查及初學者走查。

限制：不覆寫既有修改、不影響共用服務；隔離 Compose project 使用 `pangolin-m2-dev`；結束清除本次容器、network、volumes 與未使用的專用映像，保留 logs。既有 docs 服務維持運行。

驗收：必跑案例不得 skip；coverage 整體及關鍵 package 各至少 90%；build、vet、依賴／license、sqlc 再生成與 Linux race；Report 綁定 commit、source hash、工具／profile、seed 與原始證據。未通過不可標 Verified，也不可宣稱完整 M3 安全／事件交付。
