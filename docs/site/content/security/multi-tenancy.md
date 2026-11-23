---
title: 多租戶與資料隔離
description: 從已驗證的登入身份建立 Tenant Scope，讓每次資料操作有明確租戶。
---

Tenant（租戶）代表你的應用服務的一個客戶，例如不同公司。多租戶應用通常共用程序與資料庫，但每個客戶只能看到自己的資料。

## 使用順序

1. [驗證登入 Token](authentication.md)。
2. 從身份取得 Tenant，建立這次操作的 Scope。
3. 把得到的 context 傳給應用服務與 UOW。
4. Repository 與資料庫 RLS 依同一 Tenant 限制存取。

Scope 是一次操作被授權使用的資源範圍。tenant Scope 的 TenantID 與 ID 相同。

## 建立 Tenant context

以下接在身份驗證成功之後，authenticated 為上一頁的結果：

```go
tenantResolver, err := tenancy.NewResolver(authn.Identity, currentAuthority)
if err != nil { return }
tenantCtx, err := tenantResolver.Resolve(authenticated, routeTenant, headerTenant)
if err != nil { return }
registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
    Authority: currentAuthority, Permission: checkPermission,
})
if err != nil { return }
_, tenantID, ok := tenancy.Identity(tenantCtx)
if !ok { return }
bound, err := registry.Bind(tenantCtx, scope.ScopeRef{
    TenantID: tenantID, Kind: "tenant", ID: tenantID,
})
if err != nil { return }
// 將 bound 傳入你的應用服務，再由服務呼叫 UOW。
```

此為驗證成功後的 Handler 呼叫片段，import `security/scope` 與 `tenancy`。currentAuthority 是 constructor 注入的當前資格來源，實作 `Check(ctx, principal, tenant) (revision, error)`；routeTenant／headerTenant 是候選 selector，沒有時傳空字串；checkPermission 依可信 binding／action 判斷。Resolver 核對當前資格及 Token Tenant，Registry 才安裝 Scope。完整可執行的組裝見[授權範例](authorization.mdx)。

PostgreSQL 本地投影 Adapter 為 `tenancy/postgres.Authority{Pool: authorityPool}`，需先部署 Tenant migration，並由可信同步入口更新 state／revision；外部 IdP 角色 claim 與過期快取不作目前授權來源。Bookstore 原教學保留簡單 Tenant Resolver，自己的可撤權應用採用此處的 Authority 組裝。

## 接受 Tenant Header 時

Header 中的 Tenant 只當成候選值，仍須與已驗證的身份比對。Bookstore 允許 `X-Tenant-ID`，但跨租戶請求回應 403；[教學第 6 步](../tutorials/bookstore.md#6-試試跨租戶存取)可以實際測試。

預設 tenant-only 組裝不需要假的組織資料。需要在同 Tenant 內隔離團隊資源時，請閱讀[資源 Scope 與組織擴充](resource-scopes.md)；已有可選 PostgreSQL 治理 Store、Management 與 HTTP Adapter，依[組織治理指南](organizations.md)明示組裝。
