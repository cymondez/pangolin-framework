---
title: 資源 Scope 與組織擴充
description: 以完整 owner 隔離 Tenant、Enterprise 與 Organization，並了解可選 Provider 的組裝邊界。
---

Tenant 是最外層安全邊界。需要在同一 Tenant 內隔離不同團隊的資料時，可再使用資源 Scope。`ScopeRef` 包含 TenantID、Kind、ID，三個欄位共同決定單一 owner。

| Kind | ID | 可存取的業務資料 |
|---|---|---|
| tenant | TenantID | 該 Tenant 自己擁有的資料 |
| org.enterprise | 穩定的 Enterprise UUID | 該 Enterprise 自己擁有的資料 |
| org.organization | 穩定的 Organization UUID | 該 Organization 自己擁有的資料 |

父層不會自動看見子層業務資料。使用者同時加入兩個 Organization，也必須分別建立被授權的頂層操作；不能在一個 UOW 中切換 owner。

## 預設使用 Tenant

一般多租戶應用使用 [Tenant Resolver](multi-tenancy.md) 即可，不必建立假的 Organization。Bookstore 的預設 HTTP 教學維持這個組裝。

需要其他 Kind 時，使用 `scope.NewRegistry(identity, providers...)`。Registry 在啟動時拒絕重複 Kind，Bind 會核對登入身分、Tenant、Provider 回覆的完整 Scope 與 revision；自行填寫 ScopeBinding 並不會取得授權。

Provider.Authorize 與應用的 Permission／Resource Policy 仍需檢查每個動作。內建 TenantScopeProvider 的 Permission callback 預設拒絕，選取 Tenant 的資格不等於擁有所有業務權限。

## 組裝可選組織能力

`modules/organization` 將 Enterprise／Organization 能力封裝在同一個可選 Module。以下是你應用中可編譯的 `setup/organization.go`，Authority 由 constructor 明確傳入：

```go
package setup

import (
    "github.com/cymondez/pangolin-framework/kernel/module"
    "github.com/cymondez/pangolin-framework/modules/organization"
    "github.com/cymondez/pangolin-framework/security/authn"
)

func OrganizationModule(authority organization.Authority) (module.Module, error) {
    return organization.NewModule(authn.Identity, authority)
}
```

Authority 必須從受信任、前後一致的來源讀取 membership、狀態與權限。這一版提供 Provider／Module 邊界與 ScopeAware Bookstore 資料 Adapter；完整組織管理 API、預設持久化治理、撤權整合與部署遷移尚未提供，因此上述函式需搭配你明示注入的 Authority，不能單獨啟動成組織管理服務。

應用 Module 可在 RequiredScopeKinds 宣告必要能力。Host 在所有模組 Start 前核對 Registry，缺少 Provider 時不會 Ready；不支援的 Kind 回 SCOPE_KIND_UNSUPPORTED，不會退回 Tenant。

## 選擇對應的 Repository

預設 Bookstore Repository 只接受 tenant binding。`ScopedRepository` 配合 `NewScopedBooks` 與獨立的 ScopeAware schema，SQL 及 RLS 都比對完整 owner，並參照中立 resource_scopes catalog。不能只把組織 ID 填進原本的 Tenant Repository。

Schema 安裝應由你明示的部署步驟負責；API 啟動不自動改表。ScopeAware 範例目前保存同交易的事件紀錄，完整組織事件 Worker 配送尚未提供。採用前請核對[相容性](../reference/compatibility.md)與[升級說明](../reference/upgrading.md)的邊界。
