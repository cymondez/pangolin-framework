---
title: 管理 Enterprise 與 Organization（組織治理）
description: 深入了解可選 modules/organization 模組的組裝相依、成員與生命週期規則、Ownerless 復原流程、HTTP 管理路由，以及如何無痛替換持久化 Provider。
---

> **你在架構中的位置**：`可選業務基礎模組` **`[ modules/organization：Authority + contracts.Management + 管理 HTTP Handler ]`**
>
> **前置建議**：先閱讀[資源 Scope 與組織擴充](resource-scopes.md)。若你的專案只需要單純多租戶（`tenant-only`），**完全不需要安裝本模組**。

## 為什麼組織治理有這麼多嚴格狀態規則？

在多團隊企業應用中，組織管理最常釀成災難的往往是「邊界狀態」：
- 管理員誤刪一個組織的最後一位 Owner，從此沒人管得了該組織（或被誰隨便補位接管）。
- 封存（Archive）一個還有未結帳訂單、未送出事件或旗下子組織的團隊，造成孤兒資料與背景 Worker 無窮報錯。
- 未來想把組織資料從本地 PostgreSQL 換成公司既有的人事／組織系統，卻發現業務資料表全綁死了舊的組織表外鍵。

`modules/organization` 把這些治理不變條件全部收攏在單一可選模組裡，並透過 `modules/organization/contracts` 暴露穩定管理介面。

---

## 1. 組裝 `organization.Store` 與 `OrganizationModule`

`organization.NewStore(StoreOptions)` 同時實作 `organization.Authority` 與公開契約 `contracts.Management`。建立完成後直接傳入上一章的 `OrganizationModule(store)` 即可註冊 `org.enterprise` 與 `org.organization` 兩種 Kind。

啟動組裝時須明確注入以下 7 項相依（部署前須先透過[部署遷移](../data/migrations.md)安裝 Tenant projection、中立 catalog、Audit、Organization、Events 與 Idempotency）：

| `StoreOptions` 欄位 | 責任與安全邊界 |
|---|---|
| `AuthorityPool` | 專用 metadata 讀取 Pool，具備 `pangolin_scope_authority` 能力，**不具備一般業務表的 RLS bypass** |
| `Manager` | PostgreSQL UOW Manager，須透過 `events/postgres.Resource` 接上 Outbox |
| `Identity`／`Tenants` | 可信身分與當前 Tenant 資格（例如 `tenancy.Identity` 與 `tenancy/postgres.Authority`） |
| `Permissions` | 依可信 Snapshot 的 role／revision 與上層限制評估動作（預設拒絕） |
| `Effects` | 在傳入的同一筆 UOW 中保存必要 Audit 與治理 Outbox（記錄失敗必須回傳錯誤使交易回滾） |
| `UseChecker` | 檢查目標 Scope 是否仍有業務資料、未完成事件或未完成 Job（無法確認即拒絕封存） |
| `Idempotency` | 已配置結果保護與保留期限的 `idempotency.Store` |

> **安全設定**：連線池與 HTTP Server 一律由注入它們的模組擁有並關閉；`PrivateSchema` 僅能來自受信任的啟動設定，絕不接受外部請求 DTO 指定。

---

## 2. 關鍵業務規則與生命週期狀態機

```text
建立 (Active) ◀──▶ Suspend（暫停） ──▶ Archive（封存墓碑，不可逆！）
      │
      └── 若最後一位有效 Owner 在 Tenant 層被撤權 ──▶ Ownerless（持久停用）
                                                       └─ 僅能由具 tenant.scope.recover 權限者執行 Recover 恢復！
```

### 新手必知的 5 條治理規則

1. **樂觀鎖與冪等防重**：所有修改操作都必須傳入完整 `Target`、`ExpectedVersion > 0` 與非空 `IdempotencyKey`；分頁查詢 `Limit` 為 `1–100` 並透過回傳的 `cursor` 翻頁。
2. **多組織與權限隔離**：單一 User 可同時加入多個 Organization，在各組織的角色各自獨立。Enterprise Owner 的治理權**不自動包含**旗下 Organization 的內容存取權；若要成為內容成員，必須執行明示且留有 Audit 的 `JoinAsOwner`。
3. **移轉（Transfer）不改變業務 Owner**：將 Organization 從一個 Enterprise 移轉到另一個（或轉獨立）時，須同時具備轉出、來源 detach 與目標 attach 權限；移轉後 Organization ID 與其擁有的業務資料 owner 保持不變。
4. **Ownerless 防護與恢復**：一般管理 API 禁止移除最後一位有效 Owner。但若該 Owner 在更外層的 Tenant 被撤權，該組織會自動轉為持久停用的 **Ownerless** 狀態；此時單靠 Tenant 復權、`Resume` 或 `JoinAsOwner` 都**不會**自動解鎖，必須由具備 `tenant.scope.recover` 權限的管理者執行 `Recover` 指定新 Owner。已 `Archived` 的組織則是永久墓碑，不可恢復。
5. **拒絕級聯刪除與明確 HTTP 狀態碼**：仍有子 Organization、業務資料或未完成事件／Job 的 Scope **禁止封存**（不提供 `CASCADE DELETE`）。版本衝突、最後 Owner 保護與非空封存統一回 **409**；不存在或跨租戶查詢統一回 **404**（不洩漏他租戶 ID 是否存在）。

---

## 3. 掛載內建管理 HTTP Handler

只在 `organization-enabled` 應用中註冊 `modules/organization/transport/http.NewHandler(Options)`（傳入 `Store`、`Manager`、`Timeout`、`MaxBodyBytes`、`Authenticate`、`ResolveTenant`、`ResolveScope`、`Authorize` 與可選 `Rejected`）：

| 功能類別 | HTTP 路由 |
|---|---|
| 建立組織／企業 | `POST /enterprises`、`POST /organizations` |
| 分頁目錄查詢 | `GET /organizations?limit=20`、`GET /enterprises/{id}/organizations?limit=20` |
| 成員與角色管理 | `PUT` / `DELETE /organizations/{id}/memberships`、`PUT /enterprises/{id}/roles` |
| 移轉與明示加入 | `PUT /organizations/{id}/enterprise`、`POST /organizations/{id}/join-as-owner` |
| 生命週期控制 | `POST /scopes/{kind}/{id}/{suspend,resume,archive,recover}` |

- 所有寫入請求 Header 必須帶 `Idempotency-Key`；更新 JSON 須帶 `expected_version`。
- 成員與恢復操作的 Principal 格式為 `{"id": "alice", "kind": "user"}`。
- Tenant 與目標一律從可信 Binding／路由決定，DTO 拒絕額外未知欄位或自報 owner。

---

## 4. 安全替換組織持久化 Provider（零業務改動）

未來若需要替換底層組織儲存實作（同時保留既有 Scope ID、成員、狀態與公開契約），依以下標準演練執行：

1. 停止 API 接入與 Worker 認領並完成 Drain。
2. 以具 `tenant.organization.export` 權限的入口呼叫 `Export` 取得一致的 **Bundle v1** 快照（包含 ID、父層、membership、role、state、revision、grant、restriction、owners 與 catalog）。
3. 準備空私有資料區的替代 Store，以 `tenant.organization.import` 權限執行 `Import`（原子匯入資料與必要 Effects，並核對既有的中立 `resource_scopes` catalog 完全相符），再以 `Reconcile` 全量對帳。
4. 替代 Schema 須保留 Tenant qualification hook 與 ownerless／revision 契約；既有事件與 Job 會保留完整 owner 並依新 Provider 當前授權執行。
5. **不可直接降級**：只要資料庫仍有非 Tenant 業務資料或未完成工作，就不能直接移除組織模組降回 `tenant-only`——啟動期的完整 Inventory 檢查會直接擋下。

## 下一步

前往 **[HTTP 入口與稽核](http-and-audit.md)**，學習如何把整套安全檢查標準化地接進每支 HTTP API 並記錄不可抵賴的稽核日誌。
