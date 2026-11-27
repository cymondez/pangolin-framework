# Root 裝配跨套件驗收

`rootmodules_test.go` 以 Root 裝配模式（`runtime.StartRoot` ＋ kernel/app）跨套件驗證 Root 閉環與建構子 DI 的使用者可接受行為；對應 `.workflow/root-module-di/plan.md` 的驗收矩陣，正式需求 ID A01/A02/A46 不變。`legacycomposition_test.go` 重現 pre-fix 生成 app 形狀的 typed-injection 失敗（根模組自持 resource bundle ＋ custom token），保留為 foundation 修正的存在性證據。

| 測試 | 覆蓋 |
|---|---|
| TestRootDiamondPlannedOnce | Type 閉環遞推依 Type 去重；菱形共用模組只規劃一次 |
| TestRootPhaseBarrierAndStableOrder | PreConfigure→Configure→PostConfigure 完整屏障與穩定順序 |
| TestRootInvalidGraphRunsNothing | 非 root 進入之外的圖錯誤在任何 hook／建構子前拒絕 |
| TestRootVisibilityRequiresContractOwner | 無 Export 的跨模組介面不可解析；錯誤指名消費方／型別／擁有者 |
| TestRootConstructorDI | 預設 Transient、Singleton opt-in、精確具象鍵、cleanup 由 Container 擁有且 Close 反序清理 |
| TestRootNeutralReplacementKeepsIdentity | 契約擁有者 Export；安裝模組以 `As` 匯入實作並以 `ReplaceContract` 升級自身貢獻；契約 Identity 不變，消費方只相依擁有者即解析新實作 |
| TestRootFailureReverseCleanup | Start 失敗反序 Stop、不報 Ready；hook panic 仍復原視窗 |
| TestKernelAppRunDefaultsAndOverrides | kernel/app.Run 預設可啟動；caller defaults 覆寫同層、非法值在 hook 前拒絕 |
| TestLegacyRootOwnedServicesRejectTypedInjection／…RejectRegistryTypedInjection | pre-fix 形狀：根自持 bundle＋custom token 時 user module 建構子注入 `uow.UnitOfWorkManager`／`*scope.Registry` 在 seal 前失敗，錯誤指名 missing provider 與使用者模組 |
| TestLegacyExportedRootServicesStillNeedOwnerEdge | 根以 canonical token Export 後仍要求消費方宣告對擁有者（app root）的 Type 邊——生成模組無法匯入 app root，因此需要獨立 foundation 模組 |

執行：`go test ./tests/rootmodules`（無外部服務，隨一般 `go test ./...` 執行）。這些是同一 commit 的 Root 模式累積回歸入口；Mock 不作為 PostgreSQL 交易／Broker 配送語意的完成證據，正式 Gate 仍依 [工具索引](../README.md)。舊 431 個具名情境歷史不變；本套件不重編、不重命名任何既有 subtest。
