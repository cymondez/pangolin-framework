# organization/contracts

穩定的組織管理 Application Port，包含 Enterprise／Organization／Membership DTO、分頁、expected version 與冪等鍵。應用可依賴本 Port，不得依賴私有資料表或預設 Adapter。ScopeRef 仍須核對可信 binding，DTO 本身不授權。

完整 Management 實作與命令語意在 M3 交付；M2 僅有 [單一可選模組](../README.md) 與中立 Scope Provider 薄片。本 package 不取得資源、不負責清理。以 `go test ./modules/organization/...` 檢查編譯，公開 surface 另受 API inventory 保護。
