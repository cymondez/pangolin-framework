# ADR14：Settings 標準來源與中立資料存取

日期：2026-11-24。決策依使用者更正；實作與驗收狀態分開記錄，不能由本文件推論 Gate 已通過。

Settings 的基本五層為 Default、Config、Global、Tenant、User。可選 Enterprise／Organization 位於 Tenant 與 User 之間。Config 由 Configuration Reader 提供，Global 由管理來源保存，不能合併稱為 Application。既有空 Layer 位址維持 Global 資料，不搬移或重寫名稱；原有 Inherit、Scope-local User、快取版本與可信 ancestry 保留。公開 Layers 方法讓呼叫端確認標準解析順序，沒有值的來源仍可 fallback。

Feature 受 Permission 限制：先通過目前 Permission 才解析功能值。Feature 不授予權限，也不反向改寫普通 Permission 判定。既有 Evaluate 的方向正確，補測試及說明，不加入反向依賴。

資料存取的公開 Port 歸 persistence/database，PostgreSQL、MySQL、SQLite 為各自 Adapter。既有 postgres.Resource 額外安裝中立 Executor，保留 postgres.Current、交易、RLS 與事件 Resource 的完成 hooks。MySQL 使用既有鎖定的 go-sql-driver/mysql，SQLite 使用既有鎖定的 modernc.org/sqlite，不趁修正升級其他依賴。

引擎能力明示：MySQL／SQLite 沒有 PostgreSQL RLS，SQLite 也沒有同等的操作級引擎唯讀交易；要求缺少能力的 Profile 必須拒絕組裝。所有 Repository 仍帶可信完整 owner 與複合約束。既有 PostgreSQL 專屬治理／事件／Jobs／migration 不因中立 Port 出現就宣稱已移植。

DefaultOptions.DefaultIsolation 明示 Manager 未指定交易隔離時的政策，零值保留 ReadCommitted。SQLite 使用 Serializable；明示不支援的隔離級別拒絕，不靜默轉換。RequiresNew 保持獨立連線及交易，SQLite 寫鎖衝突如實回報。

修正交付需包含來源順序與授權方向回歸、真實三引擎資料存取與 UOW 契約、SQL cursor／取消／清理、各關鍵 package coverage 及同版累積 Gate。原 M5 證據只證明其綁定來源，不能拿來代替本次修正驗收。
