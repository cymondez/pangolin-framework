# ADR09 — 中立 Scope 與 Profile fixture

狀態：採用於 M0；fixture 實作待交付。對應 ENV05、ENV06、SCP-001–009、ORG-001–014。

Core 保持規範 §7.7 的 ScopeRef／PrincipalRef／ScopeBinding／Provider／Resolver；只能由 Resolver 安裝可信 binding。組織能力在單一 organization 模組；TenantScopeProvider 不建立假 Org。Domain 只持有穩定 owner ID，不依賴內建組織表。

fixture 組裝：tenant-only 無組織 implementation/schema；organization-enabled 具有完整 Enterprise／Organization；replacement-contract 用獨立測試 Provider／儲存保留相同 Scope ID、membership、state、revision 與公開管理 Port。替代 fixture 不假裝完成外部目錄 Adapter。

每組 profile 已列出 421 個具名 subtests，依 §13.4 標示 required 或附理由的 not-applicable；所有適用 Runtime 子情境保持 planned。tenant-only 的組織成功 subtests 可依 §13.4 附理由為 N/A，但未知 org hint、缺 Provider、非法降級等負測試仍必跑。不可將整組 ORG 測試 skip。

M0 的 plans／向量位於 tests/contract/subtests.json、tests/fixtures/；三個組裝的 Port fixture 位於 internal/testkit/assemblies。M0 gate 執行資料參照、適用性、API surface、純 Tenant build graph 及錯誤 N/A 的回歸檢查。真正的替換匯入、revision barrier 與前向遷移 Runtime 契約套件屬 M2／M3 正式交付。
