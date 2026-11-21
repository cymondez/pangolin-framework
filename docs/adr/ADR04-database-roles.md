# ADR04 — PostgreSQL 角色與隔離

狀態：採用於 M0；RLS／admission 未實作。對應 ENV01、TEN-001–003、SCP-001–006。

本機／CI 固定 PostgreSQL 18.7，先支援單一版本；官方映像使用 digest，資料掛載 `/var/lib/postgresql`。不將本機其他專案的 DB 當作本專案測試服務。

`pangolin_bootstrap` 只初始化 disposable 測試庫；`pangolin_owner` 為 NOLOGIN schema owner；`pangolin_migrator` 經 SET ROLE 執行部署 migration；`pangolin_app` 非 owner、非 superuser、無 BYPASSRLS；`pangolin_worker` 無通用業務表權限，只由 migration 給予必要 queue 權限。Host 用獨立授權 Port，不開全域 bypass。測試 SQL 不等於正式 migration。

新業務 schema 從第一版包含 tenant_id、FORCE RLS 與 transaction-local 設定。ScopeAware owner／catalog／複合 FK 依規範 §7.9。admission 於業務 callback 前，以單次一致權威快照核對 binding revision；撤權舊 allow 不得被 TTL 快取延續。Core 的中立 admission API 已凍結為 ScopeAdmitter.Admit(ctx, binding) (ScopeBinding, error)，只可刷新權威 revision，不可改 Ref／Principal。

驗收包括漏 SQL filter、角色屬性、跨 owner FK、migration policy OR 放寬及 Bind/admission 間撤權；皆保留真實 PG 案例，未在 M0 提前聲稱安全成立。
