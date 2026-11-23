---
title: 管理 Enterprise 與 Organization
description: 選用組織治理、明示權限與 owner 狀態，並安全替換持久化 Provider。
---

需要同一 Tenant 下的多個團隊時，先閱讀[資源 Scope](resource-scopes.md)。只有 Tenant 的應用不必安裝這個模組。以下是組裝指南，需要你自己的 PostgreSQL、身分驗證、Permission 與 Use Case；Bookstore 的預設入口仍使用 tenant-only。

## 建立 Store 與 Module

`organization.NewStore(StoreOptions)` 同時實作 Authority 與公開 `contracts.Management`。將 Store 傳給資源 Scope 頁的 `OrganizationModule` 函式即可註冊兩種 Kind。相依由你的啟動程式明示提供：

| 選項 | 使用方式 |
|---|---|
| AuthorityPool | 專用 metadata 讀取 Pool；具有 pangolin_scope_authority 角色能力，沒有一般業務表 bypass |
| Manager | PostgreSQL UOW，透過 events/postgres.Resource 接入 Outbox |
| Identity／Tenants | 可信身分及目前 Tenant 資格；可用 tenancy.Identity 與 tenancy/postgres.Authority |
| Permissions | 依可信 Snapshot 的 role／revision、上層限制判斷 action；預設拒絕 |
| Effects | 在傳入的同一 UOW 保存必要 Audit 與治理 Outbox；錯誤必須回傳 |
| UseChecker | 檢查業務資料／未完成事件與 Job，無法確認就拒絕封存 |
| Idempotency | 已設定結果保護與期限的冪等 Store |

先透過[部署遷移](../data/migrations.md)安裝 Tenant projection、中立 catalog、Audit、Organization、Events 與 Idempotency。連線與 Server 由注入它們的 Module 擁有並關閉。`PrivateSchema` 只能由可信部署設定，不接受 HTTP DTO 指定。

## 常用操作

Management 提供建立 Enterprise、獨立或附屬 Organization、分頁目錄、組織 membership、Enterprise role、移轉及 suspend／resume／archive。更新傳入完整 Target、`ExpectedVersion > 0` 與非空 `IdempotencyKey`；列表 Limit 為 1–100，使用回傳 cursor 繼續下一頁。

User 可加入多個 Org，各自角色獨立。Enterprise owner 的 metadata 治理權不會讓他讀取 Org 內容；`JoinAsOwner` 才是明示且有必要稽核的加入流程。移轉要求 Org transfer、來源 detach 與目標 attach 權限，Org ID 與業務 owner 保持不變。

一般管理不能移除最後有效 owner。Tenant 撤權仍可執行；最後 owner 失效後，scope 持久停用為 ownerless。Tenant 復權、Resume 或 JoinAsOwner 不會自動恢復；只有具 `tenant.scope.recover` Permission 的 `Recover` 可重新建立有效 owner。Archived 墓碑不能恢復。

有子 Org、業務資料或未完成工作的 scope 不能封存，不會 cascade delete。版本衝突、最後 owner 與非空衝突回 409；不存在或跨 Tenant lookup 統一 404。冪等重播仍重新驗證當前 action／target 權限。

## 接上 HTTP

`modules/organization/transport/http.NewHandler(Options)` 需要 Store、Manager、Timeout、MaxBodyBytes、Authenticate、ResolveTenant、ResolveScope、Authorize，可加 Rejected。只在 organization-enabled 應用註冊。

| 目的 | 路由 |
|---|---|
| 建立 | `POST /enterprises`、`POST /organizations` |
| 分頁目錄 | `GET /organizations?limit=20`、`GET /enterprises/{id}/organizations?limit=20` |
| Membership／role | `PUT` 或 `DELETE /organizations/{id}/memberships`、`PUT /enterprises/{id}/roles` |
| 移轉／明示加入 | `PUT /organizations/{id}/enterprise`、`POST /organizations/{id}/join-as-owner` |
| 生命週期 | `POST /scopes/{kind}/{id}/{suspend,resume,archive,recover}` |

寫入 Header 需 `Idempotency-Key`。更新 JSON 帶 `expected_version`；建立使用 `slug` 與可選 `enterprise_id`。Membership／recovery 的 principal 是 `{ "id": "alice", "kind": "user" }`，role 由相應操作提供。Tenant 與 target 從可信 binding／路由決定，DTO 不接受 owner 或額外欄位。

## 替換 Provider

先停止接入／認領並 Drain，以 `Export` 取得一致快照，使用 Bundle v1 保存 ID、父層、membership、role、state、revision、grant、restriction、owners 與 catalog。替代 Store 的 `Import` 需要空私有資料區及 `tenant.organization.import`；`Reconcile` 核對完整資料，Export 需要 `tenant.organization.export`。

資料與必要 Effects 原子匯入；既有中立 catalog 必須完全相符。替代 schema 也須安裝 Tenant qualification hook，保留 ownerless／revision 契約。事件與 Job 保留完整 owner，並以新 Provider 的當前 grant 執行。已有非 Tenant 資料或未完成工作時不能直接改成 tenant-only；啟動需檢查完整 inventory。

下一步：[可靠事件](../events/reliable-delivery.md)，讓治理變更與通知同交易保存。
