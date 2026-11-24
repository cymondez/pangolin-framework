# Tenant Resolution

`NewResolver(identityReader, authority)` 將已驗證身分與目前 Tenant 資格核對；`Resolve(ctx, routeHint, headerHint)` 拒絕相互衝突或超出 Token Tenant 的 selector。HTTP、Event 與 Job 入口各自建立新 context，不保存已結束的 HTTP context。Host 跨租戶工作使用獨立治理入口。

組裝：Authentication → Resolver.Resolve → `scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: authority, Permission: check})` → Registry.Bind。`Authority.Check` 必須回傳目前有效資格的不透明 revision；無法確認時回 `scope.ErrAuthorityUnavailable`，撤權時回 `scope.ErrDenied`。Provider 在每次新根 admission 重新檢查，RequiresNew 延用父操作快照。Permission callback 預設缺少即拒絕；服務帳號亦需獨立有效 grant。

套件不持有外部資源。PostgreSQL projection Adapter 見 [postgres](postgres/README.md)。`go test ./tenancy/...` 驗證 selector／資格邊界，真實資料庫案例在 `tests/m3`。外部投影的上游新鮮度由組裝者保證，未確認不得回傳過期 allow。
