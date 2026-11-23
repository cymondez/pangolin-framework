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

NewTenantResolver／TenantScopeProvider 保留簡單可信 IdentityReader 的靜態組裝，不能把固定 revision 當可變 membership 權威。需要目前 Tenant 資格與撤權時，使用 tenancy.Resolver 與 tenancy.Provider／PostgreSQL projection；每次新根 UOW 在 Begin 前呼叫保存於 private context 的 ScopeAdmitter。

驗證：`go test ./security/scope ./security/authn ./persistence/uow`；跨租戶與 RLS 用 [真實 M1 測試](../../docs/guides/m1-development.md)。

## Provider 組裝

`NewRegistry(identity, providers...)` 在建構時拒絕重複 Kind，封存 provider map；Bind 比對可信 Principal／Tenant、完整 Ref 與 revision 後才安裝 private-key binding。`Require` 檢查能力，Runtime 在所有 Start 前核對模組 RequiredScopeKinds。tenant-only 未支援的 Kind 回 SCOPE_KIND_UNSUPPORTED，不做 fallback。

內建 `TenantScopeProvider` 只驗證 tenant 選取資格；Authorize 須明示 Permission callback，預設拒絕。`Registry.Admit` 重新核對權威 Ref／Principal／revision；Required／RequiresNew 共用父 admission 快照，RequiresNew 不重新延長授權。組織能力由單一可選 [organization](../../modules/organization/README.md) 提供兩種 Kind，Core 不 import 該模組。故障使用 SCOPE_AUTHORITY_UNAVAILABLE，不重用 stale allow。

`Select` 比對 route／header、明示必選 Scope 與 tenant-only 預設；`MatchOwner` 拒絕 DTO 搬移 owner。Binding 值不是 bearer token，直接 Provider.Authorize 也要核對可信 context。`CheckInventory` 配合 Runtime.StartupChecks，在 Start 前拒絕仍有不支援 owner 或未完成工作之部署；Inventory 必須覆蓋 catalog／業務資料／工作，不用過濾後的空結果宣告可降級。
