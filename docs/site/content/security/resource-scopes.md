---
title: 資源 Scope 與組織擴充（Resource Scopes）
description: 理解 ScopeRef (TenantID, Kind, ID) 的精確 owner 隔離模型，以及如何透過中立 Port 按需組裝可選的 modules/organization 模組。
---

> **你在架構中的位置**：`Tenant（最外層客戶邊界）→` **`[ ScopeRef = (TenantID, Kind, ID) 精確資源擁有者 ]`** `→ UOW + Repository + RLS`
>
> **前置建議**：先閱讀[多租戶與資料隔離](multi-tenancy.md)與[權限與資源政策](authorization.mdx)。

## 為什麼在 Tenant 之外還需要「資源 Scope」？

對許多 B2B SaaS 來說，一個客戶（Tenant）底下還會有更細的隔離需求：例如一個集團（Enterprise）底下有多間獨立分公司（Organization），各分公司只能存取自己的業務資料，不能混在一起。

但同時，**不是所有專案都需要組織架構**！如果把 Organization 寫死進框架核心，單純的 Tenant-only 應用就會背負一堆無用的資料表與假組織。

panGOlin 的解法是 **中立的 `ScopeRef` 模型 + 可選的 `modules/organization` 模組**：
- **框架核心（UOW、Repository、Events、Jobs）只認識中立的 `security/scope.ScopeRef`**，完全不 `import` 組織實作。
- **`ScopeRef = (TenantID, Kind, ID)`** 精確指定「單一資料擁有者」：

| Scope `Kind` | `ID` 代表什麼 | 可存取的業務資料範圍 |
|---|---|---|
| `tenant` | 等於 `TenantID` | 僅限該 Tenant 自己擁有的資料 |
| `org.enterprise` | 穩定的 Enterprise UUID | 僅限該 Enterprise 自己擁有的資料 |
| `org.organization` | 穩定的 Organization UUID | 僅限該 Organization 自己擁有的資料 |

### 精確 Owner 隔離（最容易誤解的核心規則！）

```text
Tenant A
 ├── Tenant A 自己的資料          ◀── 僅 tenant/A 可存取
 ├── Enterprise E1 的資料         ◀── 僅 org.enterprise/E1 可存取（❌ 不能自動讀 Org 1/2 的資料！）
 │     ├── Organization 1 的資料  ◀── 僅 org.organization/Org1 可存取
 │     └── Organization 2 的資料  ◀── 僅 org.organization/Org2 可存取
 └── 獨立 Organization 3 的資料    ◀── 僅 org.organization/Org3 可存取（無隸屬 Enterprise）
```

- **父層絕不是子層資料的聯集**：Enterprise 治理角色不會自動看到底下 Organization 的業務資料。
- **同一 UOW 內不可切換 Owner**：即使使用者同時加入 Org 1 與 Org 2，要操作兩邊的資料也必須分別開啟各自授權的頂層操作，不能在同一個 UOW 中偷偷換 Scope。

---

## 1. 兩種部署組裝模式怎麼選？

1. **`tenant-only`（預設，最簡單）**：不需要建立任何假 Organization，直接使用[多租戶指南](multi-tenancy.md)的 Tenant Resolver 即可。
2. **`organization-enabled`（進階多層組織）**：同時掛入 `modules/organization` 與 ScopeAware Schema，透過 `scope.NewRegistry(identity, providers...)` 註冊三種 Kind。
   - 每種 Kind 全系統只能有一個 Provider（重複註冊啟動前即拒絕）。
   - `Bind` 會完整核對登入身分、Tenant、Provider 回覆的完整 Scope 與 revision；**未註冊的 Kind 一律回 `SCOPE_KIND_UNSUPPORTED`，絕不靜默退回 `tenant`**。

---

## 2. 組裝可選的組織模組：`setup/organization.go`

`modules/organization` 把 Enterprise、Organization、Membership 與治理能力完整封裝在單一可選模組中。以下是你應用中可直接編譯的 `setup/organization.go`：

```go
package setup

import (
	"github.com/cymondez/pangolin-framework/kernel/module"
	"github.com/cymondez/pangolin-framework/modules/organization"
	"github.com/cymondez/pangolin-framework/security/authn"
)

// OrganizationModule 建立可選的組織治理模組。
// authority 必須由啟動端明確注入可信的當前資料來源（例如 organization.NewStore），
// 模組本身會註冊 org.enterprise 與 org.organization 兩種 Scope Kind。
func OrganizationModule(authority organization.Authority) (module.Module, error) {
	return organization.NewModule(authn.Identity, authority)
}
```

### 啟動前能力守門：`RequiredScopeKinds`

如果你的某個業務模組（例如 `billing`）必須在 `org.organization` Scope 下運作，請在該模組宣告 `RequiredScopeKinds`。Host 會在所有模組 `Start` 之前核對 `scope.Registry`——萬一組裝端忘了掛上組織模組，應用會在啟動前就拒絕進入 Ready，不會等到線上收到請求才爆炸。

---

## 3. Repository 與 Schema 必須成對切換

請特別留意：**不能直接把 `org.organization` 的 ID 塞進原本的 TenantOnly Repository！**

- **TenantOnly 模式**：使用一般只驗 `tenant` binding 的 Repository（例如預設 Bookstore 的 `NewBooks`）。
- **ScopeAware 模式**：
  - 業務資料表須具備完整 `(tenant_id, owner_scope_kind, owner_scope_id)` 欄位，建立含完整 owner 的複合唯一鍵與外鍵，並參照中立的 `resource_scopes` catalog（**不綁死組織模組私有表**，保留未來替換組織 Provider 的彈性）。
  - 搭配 ScopeAware Repository 與服務（如 Bookstore 範例的 `ScopedRepository` + `NewScopedBooks`），讓 SQL 與 PostgreSQL RLS 同時比對完整三欄 owner。
  - Schema 由[部署資料庫遷移](../data/migrations.md)安裝，API 啟動前執行相容性與完整 Scope Inventory 檢查。

---

## 4. 新頂層 UOW 與撤權防護

每一次開啟**新的頂層 UOW** 之前，框架都會向權威來源重新核對當前的 `revision`：
- 如果使用者在取得 Binding 後、進入 UOW 前被管理員移出該組織，UOW 准入階段會立刻攔截拒絕。
- 已經通過准入的短 UOW 則依當下快照完成，`Required` 與 `RequiresNew` 均保留父 binding 與期限。

## 下一步

需要完整的組織建立、成員管理、移轉、Ownerless 復原與 Provider 替換功能？繼續閱讀 **[管理 Enterprise 與 Organization](organizations.md)**。
