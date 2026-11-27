---
title: 多租戶與資料隔離（Multi-Tenancy）
description: 理解 panGOlin 如何將外部 Tenant 提示視為候選值、向權威來源即時確認資格，並結合 Scope Binding 與 PostgreSQL RLS 徹底杜絕跨租戶洩漏。
---

> **你在架構中的位置**：`Authenticate（已驗身分）→` **`[ Resolve Tenant（核對當前資格）→ Bind ScopeRef ]`** `→ Permission → UOW（寫入 RLS）→ Repository`
>
> **前置建議**：先閱讀[可擴充登入與身分驗證](authentication.md)。

## 為什麼多租戶系統不能「只信 JWT 裡的 `tenant_id` 或 `X-Tenant-ID`」？

開發 SaaS 多租戶系統時，最常見的兩個嚴重安全漏洞是：
1. **盲信請求 Header（`X-Tenant-ID`）**：攻擊者拿自己在公司 A 的合法 Token，但把 Header 改成 `X-Tenant-ID: company-b`，就偷看到公司 B 的資料。
2. **只信靜態 JWT、未查即時撤權**：員工離職或客戶停權後，舊 JWT 在過期前（例如還有 15 分鐘）依然可以繼續讀寫公司機密資料。

panGOlin 用四道連續關卡堵死這兩個漏洞：

```text
[1. 登入驗證]   取得已驗證的 Principal（如 alice）與 Token 內的 tenant hint
      │
      ▼
[2. Tenant Resolver]  把路由參數／Header 當作「候選值」與已驗證身分比對；
      │               同時向「當前資格權威 Authority」即時查詢 (alice, tenant-a) 是否仍有效 + 取得 revision
      ▼
[3. Scope Registry]   確認通過後，透過私有 context key 安裝不可變的 ScopeBinding (TenantID, Kind="tenant", ID=TenantID)
      │
      ▼
[4. UOW + RLS]        開啟頂層 UOW 前再核對一次 revision，交易內寫入 transaction-local RLS，
                      且 Repository SQL 強制帶入 database.CurrentOwner(ctx) 條件
```

---

## 1. 建立受信任的 Tenant Scope context

以下程式碼片段接在上一頁[身分驗證](authentication.md)成功（取得 `authenticated` context）之後執行，需 import `security/scope` 與 `tenancy`：

```go title="HTTP 入口解析並綁定可信 Tenant Scope ✅"
// 步驟 1：建立 Tenant Resolver。
// - authn.Identity（或 identity.Identity）：讀取已驗證的身分
// - currentAuthority：由建構子注入的「當前資格來源」，實作 Check(ctx, principal, tenant) (revision, error)
tenantResolver, err := tenancy.NewResolver(authn.Identity, currentAuthority)
if err != nil {
	return
}

// 步驟 2：傳入候選值（routeTenant、headerTenant；沒有則傳空字串）。
// Resolver 會拒絕跨租戶偽造、拒絕衝突的候選值，並向 currentAuthority 確認未被撤權。
tenantCtx, err := tenantResolver.Resolve(authenticated, routeTenant, headerTenant)
if err != nil {
	return
}

// 步驟 3：建立 Scope Registry，掛載內建的 tenancy.Provider（並注入權限檢查函式）。
registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
	Authority:  currentAuthority,
	Permission: checkPermission,
})
if err != nil {
	return
}

// 步驟 4：從已核准的 tenantCtx 讀出唯一可信的 tenantID，綁定成不可變 ScopeBinding。
// 注意：對 tenant Scope 而言，Ref 的 TenantID 與 ID 永遠相同，Kind 為 "tenant"。
_, tenantID, ok := tenancy.Identity(tenantCtx)
if !ok {
	return
}
bound, err := registry.Bind(tenantCtx, scope.ScopeRef{
	TenantID: tenantID,
	Kind:     "tenant",
	ID:       tenantID,
})
if err != nil {
	return
}

// 步驟 5：將綁定完成的 bound context 傳入 Application Service（再由服務開啟 UOW）。
```

> 想看完整可執行的獨立 `main.go`？請直接執行下一章 **[權限與資源政策](authorization.mdx)** 的 `go run ./authorization`。

---

## 2. 什麼是 `currentAuthority`（當前資格來源）？

- **內建 PostgreSQL 實作**：`tenancy/postgres.Authority{Pool: authorityPool}`。部署前須先透過[部署遷移](../data/migrations.md)安裝 `tenancy/postgres.Migrations`，並由可信的同步入口更新成員 `state` 與 `revision`。
- **為什麼不能拿外部 IdP Token 裡的角色 claim 或過期快取充當 Authority？**
  因為 IdP 簽發的 JWT 是過去時間點的快照，無法即時反映「5 秒前管理員剛把此人移出租戶」的事實。
- **與 Bookstore 基礎範例的差異**：套件內的基礎 Bookstore 示範為簡化入門而使用簡易 `NewTenantResolver`；當你要開發**支援即時撤權**的正式應用時，請務必採用本頁的 `tenancy.NewResolver` + `tenancy/postgres.Authority` 組裝。

---

## 3. Header 與跨租戶防禦實測

如果你允許前端傳入 `X-Tenant-ID` 或路由 `/tenants/{tenantId}/...`，請牢記：
- **所有來自 HTTP 的租戶資訊永遠只是「候選值（Hint）」**，絕不可直接拿去查資料庫。
- 當候選值與已驗證身分不符（或無權存取該租戶）時，框架一律拒絕並回傳 **HTTP 403**。
- 你可以直接在 [Bookstore 教學第 6 步](../tutorials/bookstore.md) 親手用 `alice` 的 Token 帶入 `X-Tenant-ID: tenant-b`，觀察請求確實被攔截為 `403`。

---

## 4. 常見誤區

- **❌ 誤區：為了以後可能支援組織，先在 `tenant-only` 專案建立一堆假的預設 Organization**
  完全不需要！panGOlin 預設就是乾淨的 `tenant-only` 模式。等你真的需要在同一個客戶（Tenant）底下切分多個互相隔離的團隊或子公司時，再依[資源 Scope 與組織擴充](resource-scopes.md)與[組織治理指南](organizations.md)平滑升級即可。
- **❌ 誤區：自己組一個 `scope.ScopeBinding{...}` 結構就想跳過驗證**
  `ScopeBinding` 透過私有 context key 受控安裝，自行建立 struct 無法取得授權，也不會通過 UOW 與 Repository 檢查。

## 下一步

確定了「誰在操作哪個租戶」之後，接著閱讀 **[權限與資源政策](authorization.mdx)**，學會判斷「他現在能不能做這個動作」。
