# Scope：中立 Port 與 tenant-only binding

`ScopeRef = (TenantID, Kind, ID)` 識別單一資源 owner。Core 只依賴本套件的 [中立 Port](contracts.go)，不 import organization 模組。M1 實作見 [tenant.go](tenant.go)。

```go
resolver := scope.NewTenantResolver(authn.Identity)
bound, err := resolver.Bind(authenticated, scope.ScopeRef{
    TenantID: tenantHint, Kind: "tenant", ID: tenantHint,
})
// 先檢查 err；bound 才能進入 UOW。
```

`IdentityReader` 由可信組裝端提供，應讀取 Authentication 已驗證的 identity。Hint 的 Tenant 必須等於身分 Tenant；M1 只接受 tenant Kind，且 Scope ID 必須等於 TenantID。Org／Enterprise hint 或 owner 衝突均拒絕，不降級成 tenant Scope。

Binding 透過 private key 安裝；`Current(ctx)`／Resolver.Current 只讀取已安裝的 binding。應用自行建立 ScopeBinding 值或在 DTO 填 owner 不會取得 Scope。UOW 保持同一 binding，拒絕巢狀更換。

M1 revision 固定為 tenant-only fixture 的標記，不代表已有可變 membership 的權威 revision 查驗。完整 Provider 唯一性、ScopeAdmitter、撤權與 Organization profile 契約按 M2／M3 交付。

驗證：`go test ./security/scope ./security/authn ./persistence/uow`；跨租戶與 RLS 用 [真實 M1 測試](../../docs/guides/m1-development.md)。
