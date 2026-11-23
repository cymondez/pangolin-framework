# 分層 Settings

`New(provider, cache, ancestry, ttl, definitions...)` 明確注入來源、Scoped Cache 與可信 ancestry Port。Definition 宣告 Default、Inherit 及可選值驗證。Resolve 回傳值、版本與按優先序排列的候選值。

Tenant 為 User-in-Tenant > Tenant > Application > Default。Org 為 User-in-Scope > Org > 目前 Enterprise（若有）> Tenant > Application > Default；Enterprise 沒有 Org 層。未宣告 Inherit 的 key 僅讀目前 User／Scope 與 Default。ancestry 是受信基礎設施，不能由 request DTO 提供。未知 scope kind、無可信 binding 或 ancestry 故障一律拒絕。

MemoryProvider 是本機組態來源；Put 是僅供已授權管理組裝呼叫的 Port，更新後發布含完整 owner 的 Change。Update 只寫目前 Scope，先驗 settings.write。通知／刪除失敗會回傳已保存的新版本及 cause；Resolve 即使命中 Cache 也核對原子來源 snapshot 版本，來源故障不服務舊值。跨程序持久化需另實作 Provider／Publisher；Memory 不冒稱可在 PostgreSQL UOW 內回滾。

組裝擁有 Cache／Provider；沒有背景資源需關閉。`go test ./platform/settings` 驗證覆寫、完整 Scope、失效失敗與版本收斂。
