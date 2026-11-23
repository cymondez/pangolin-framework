# organization/contracts

穩定的組織管理 Application Port，包含 Enterprise／Organization／Membership DTO、分頁、expected version 與冪等鍵。應用可依賴本 Port，不得依賴私有資料表或預設 Adapter。ScopeRef 仍須核對可信 binding，DTO 本身不授權。

預設 PostgreSQL Management 位於 [單一可選模組](../README.md)，包含治理 mutation、Tenant 資格／role／state 授權、同交易 Audit／Outbox 與冪等結果。更新須提供正確 expected version 及非空冪等鍵；分頁 limit 為 1–100，cursor 不透明。DTO 的 Tenant／target 仍需與可信情境及逐 action Policy 核對，不能藉傳入 DTO 取得資料權限。

本 package 不取得資源、不負責清理。以 `go test ./modules/organization/...` 驗證；真實治理／替換契約在 `tests/m3`，公開 surface 另受 API inventory 保護。正式完成狀態以同 commit 累積 Gate 與證據為準。
