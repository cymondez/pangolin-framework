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
_, tenantID, ok := authn.Identity(authenticated)
if !ok { return }
resolver := scope.NewTenantResolver(authn.Identity)
bound, err := resolver.Bind(authenticated, scope.ScopeRef{
    TenantID: tenantID, Kind: "tenant", ID: tenantID,
})
if err != nil { return }
// 將 bound 傳入你的應用服務，再由服務呼叫 UOW。
```

額外 import `github.com/cymondez/pangolin-framework/security/scope`。Bind 成功後才能進入需要授權的資料操作；自己填寫 ScopeBinding struct 不會取得權限。

## 接受 Tenant Header 時

Header 中的 Tenant 只當成候選值，仍須與已驗證的身份比對。Bookstore 允許 `X-Tenant-ID`，但跨租戶請求回應 403；[教學第 6 步](../tutorials/bookstore.md#6-試試跨租戶存取)可以實際測試。

預設 tenant-only 組裝不需要假的組織資料。需要在同 Tenant 內隔離團隊資源時，請閱讀[資源 Scope 與組織擴充](resource-scopes.md)；目前提供 Provider／Module 組裝邊界，完整組織治理尚未提供。
