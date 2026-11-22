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

M1 revision 固定為 tenant-only fixture 的標記，不代表已有可變 membership 的權威 revision 查驗。Provider 唯一性與 profile 組裝已在 M2 擴充；ScopeAdmitter 的 UOW admission／撤權契約在 M3 驗收。

驗證：`go test ./security/scope ./security/authn ./persistence/uow`；跨租戶與 RLS 用 [真實 M1 測試](../../docs/guides/m1-development.md)。

## Provider 組裝

`NewRegistry(identity, providers...)` 在建構時拒絕重複 Kind，封存 provider map；Bind 比對可信 Principal／Tenant、完整 Ref 與 revision 後才安裝 private-key binding。`Require` 檢查能力，Runtime 在所有 Start 前核對模組 RequiredScopeKinds。tenant-only 未支援的 Kind 回 SCOPE_KIND_UNSUPPORTED，不做 fallback。

內建 `TenantScopeProvider` 只驗證 tenant 選取資格；Authorize 須明示 Permission callback，預設拒絕。`Registry.Admit` 提供權威 Resolve 核對，UOW 自動 admission／撤權競態與完整預設 Organization Authority 仍由 M3 接入並驗收。組織薄片由單一可選 [organization](../../modules/organization/README.md) 提供兩種 Kind，Core 不 import 該模組。故障使用 SCOPE_AUTHORITY_UNAVAILABLE，不重用 stale allow。
