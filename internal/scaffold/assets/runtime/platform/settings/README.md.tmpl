# 分層 Settings

`New(provider, cache, ancestry, ttl, definitions...)` 明確注入來源、Scoped Cache 與可信 ancestry Port。Definition 宣告 Default、Inherit 及可選值驗證。Resolve 回傳值、版本與按優先序排列的候選值。

基本五層由低至高為 `Default < Config < Global < Tenant < User`。Config 來自 Configuration Reader；Global 是可更新的全域儲存來源，兩者獨立。不得將它們合併為 Application 層。

| 目前 Scope | 由低至高的完整來源 |
|---|---|
| Tenant | Default → Config → Global → Tenant → User-in-Tenant |
| Enterprise | Default → Config → Global → Tenant → Enterprise → User-in-Scope |
| Organization（獨立） | Default → Config → Global → Tenant → Organization → User-in-Scope |
| Organization（隸屬 Enterprise） | Default → Config → Global → Tenant → Enterprise → Organization → User-in-Scope |

Enterprise／Organization 是可選層；tenant-only 不建立假組織層。Resolve 由高至低取第一個有效候選值。未宣告 Inherit 的 key 僅讀目前 User／Scope 與 Default。ancestry 是受信基礎設施，不能由 request DTO 提供。未知 scope kind、無可信 binding 或 ancestry 故障一律拒絕。

`service.Layers(ctx, key)` 回傳該定義由高至低的標準解析順序，使用 `LayerUser`、`LayerOrganization`、`LayerEnterprise`、`LayerTenant`、`LayerGlobal`、`LayerConfig`、`LayerDefault` 常數。回傳切片可安全供呼叫端使用；這是解析順序，不表示每一層都有值。Config 需明示接上 WithConfiguration，沒有值時繼續 fallback。

既有空 `Layer{}` 位址代表 Global；本次沒有搬移既有資料或改寫 Setting 名稱。全域管理寫入仍由受信組裝使用 MemoryProvider.Put；一般 Service.Update 僅可更新目前精確 Scope。

MemoryProvider 是本機組態來源；Put 是僅供已授權管理組裝呼叫的 Port，更新後發布含完整 owner 的 Change。Update 只寫目前 Scope，先驗 settings.write。通知／刪除失敗會回傳已保存的新版本及 cause；Resolve 即使命中 Cache 也核對原子來源 snapshot 版本，來源故障不服務舊值。跨程序持久化需另實作 Provider／Publisher；Memory 不冒稱可在 PostgreSQL UOW 內回滾。

組裝擁有 Cache／Provider；沒有背景資源需關閉。`go test ./platform/settings` 驗證覆寫、完整 Scope、失效失敗與版本收斂。

`WithConfiguration(baseProvider, reader, converters)` 使用精確前綴 `reader.Lookup("Settings:" + settingName)` 查詢組態值；不拆分、重寫或自動補 index，點號仍是名稱本身。Config 位於 Global 與 Default 之間；現有 Scope／User／Global 儲存值優先，Inherit=false 不讀此上層。預設將 Configuration 的字串編碼成 JSON string，即使內容像數字或 JSON 也不猜型別；typed Feature 必須明示 converter。既有 JSON RawMessage 管理 API 保留，字串組態在此邊界明確轉換。null／空物件繼續 fallback，空陣列的空字串是有效覆寫。Source revision 參與快取核對；Update 保持完整原名與原 owner，轉交原儲存 Provider。

解析規則固定以高至低層級取第一個非空候選值，Inherit 與完整 Scope 隔離一律按本專案的明示契約控制。
