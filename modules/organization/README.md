# organization

單一可選模組，封裝 Enterprise、Organization、Membership 與治理。產品版本為 `0.1.0`；`NewModule(identity, authority)` 一次註冊 `org.enterprise`／`org.organization`。核心只依賴 [中立 Scope Port](../../security/scope/README.md)，tenant-only 組裝不 import 本模組、不建立組織 migration 或路由。公開管理介面維持 [contracts.Management](contracts/README.md)，應用不能直接操作本模組的私有表。

## 組裝與公開 API

`NewStore(StoreOptions)` 提供預設 PostgreSQL Authority 與完整 `contracts.Management`。必填相依如下：

| 選項 | 責任 |
|---|---|
| `AuthorityPool` | 治理 metadata 專用唯讀 infrastructure pool；可用 `pangolin_scope_authority` 成員 login，沒有業務表 bypass |
| `Manager` | PostgreSQL UOW；可靠事件透過 `events/postgres.Resource` 接上 Outbox 完成階段 |
| `Identity`／`Tenants` | 已驗證 Principal／Tenant 與目前 Tenant 資格；例如 `tenancy.Identity` 及 `tenancy/postgres.Authority` |
| `Permissions` | 依 Snapshot 的 scope、role、revision 執行 Permission／Resource Policy；未明示允許必須拒絕，Feature 不授權 |
| `Effects` | 在傳入的同一 UOW context，以 `platform/audit.Recorder` 保存必要 Audit，並以 `events/outbox.Publisher` 保存版本化治理事件 |
| `UseChecker` | 經公開 inventory Port 檢查業務 owner／未完成事件與 Job；無法確認時拒絕 archive |
| `Idempotency` | 有界 key／body、retry window／retention 及結果保護函式的 `persistence/idempotency.Store` |

`PrivateSchema` 預設 `pangolin_organization`，只能在部署時設定合法 SQL identifier；替代 Adapter 可用自己的私有 schema。中立 catalog 始終是 `pangolin.resource_scopes`，業務 owner ID 不隨私有 schema 更換。Store 不關閉注入的 pool，生命週期由 DI owner 負責。

Management 支援建立 Enterprise／standalone 或附屬 Organization、分頁 metadata 目錄、獨立組織角色、Enterprise 治理角色、轉移、suspend／resume／archive。更新要求 `ExpectedVersion > 0` 及非空 `IdempotencyKey`；list 的 Limit 為 1–100，按穩定 ID 排序，使用不透明 cursor。`JoinAsOwner` 是明示且受稽核的 Enterprise owner 加入流程；Enterprise 角色本身不取得 Org 內容權限。`Recover` 需要 Tenant 的 `tenant.scope.recover` Permission，建立有效 owner；不能復活 archived 墓碑。

## 授權、交易與失敗

預設 Authority 以單一 SQL visibility snapshot 讀取本地 `pangolin_security.tenant_memberships`、scope state、有效 owner、membership／service grant、目前父層及 deny。Tenant／Enterprise restriction 只收緊下層權限。角色不擴散到同 Tenant 其他 Org；Enterprise metadata 目錄不代表業務資料聯集。Service 必須有明示 scope grant。

Resolver／新根 UOW admission 向權威重新解析，JWT 的組織聲明與 TTL 快取不作授權來源。已 admission 的短 UOW 使用包含 Tenant／parent revision 的不可變簽署快照；RequiresNew 保留父 binding／期限。權威故障回 `SCOPE_AUTHORITY_UNAVAILABLE`，不使用舊 allow 或降級成 Tenant。簽署快照只用於當前受控 context，不是可由 DTO 安裝的 bearer token，也不供跨程序保存。Permission Adapter 應依此 immutable Snapshot 決策。

治理 mutation 在同一 UOW 鎖定並比較 catalog version，更新私有資料／owner／catalog revision、必要 Audit 與 Outbox。任一步驟失敗會回滾，不自行重跑 callback。治理事件保留發起操作的 ScopeRef，受影響的 Org／Enterprise 放在 `Change.Target`／payload，不能在完成途中切換 owner。組裝時須註冊 schema version 1：`enterprise.created`、`organization.created`、`organization.membership.changed`、`organization.membership.removed`、`enterprise.role.changed`、`organization.transferred`、`scope.suspended`、`scope.active`、`scope.archived`、`organization.provider.imported`。

冪等結果重播前重新驗證當前 action／target 權限，撤權後不能藉舊 key 讀成功結果。一般管理不能移除最後一位有效 owner；Tenant 撤權不被阻擋，ownerless scope 對新授權不可用。轉移要求 Org transfer、來源 detach、目標 attach 權限，保留 Org／業務 owner ID。有子 Org 的 Enterprise、有業務資料或未完成工作的 scope 回衝突，不 cascade delete。

錯誤保留 cause，可用 `errors.Is` 判斷 `ErrNotFound`、`ErrConflict`、`ErrLastOwner`、`ErrInUse` 及中立 Scope／UOW／Idempotency 錯誤。對外 lookup 統一 404、不暴露跨 Tenant 存在性；可識別的 action 拒絕為 403；版本／最後 owner／非空衝突為 409。Commit Unknown 仍由 UOW 保存 Unknown，不因後續清理而宣稱未提交。

## 部署、替換與 HTTP

`Migrations()` 是 Goose SQL fs.FS，需與 Tenant projection、中立 catalog、Audit、Events、Idempotency 以全域部署鎖及相依順序合併套用。API 只檢查相容性，不自行執行 DDL。私有表啟用 FORCE RLS；應用角色只在可信 Tenant 控制面 UOW 內存取，專用權威角色只有 metadata SELECT。

Provider 切換先停止認領並 Drain，再以 `Export` 取得同一 repeatable-read 快照。Bundle version 1 保存穩定 IDs、parent、完整 membership／role／state／revision、service grant、restriction、owners 與中立 catalog。`EncodeBundle`／`DecodeBundle` 定義邏輯交換格式，檔案傳送、存取控制及加密由部署工具負責。`Import` 要求具 `tenant.organization.import` Permission 的 Tenant context 與空私有資料區；既有 catalog ID／state／version 必須完全相同。全部匯入資料與必要 Effects 原子保存，`Reconcile` 比對完整資料。既有事件／Job 留在中立 store，部署 inventory 另對帳其 owner 及未完成工作，不能刪除或轉成 Tenant owner。Export 需要 `tenant.organization.export`。

可選的 [HTTP Adapter](transport/http/README.md) 提供控制面路由、大小與 timeout 限制、驗證／授權／解碼順序及 Problem Details，仍由 Management 自身維持敏感操作授權。

## 驗證與交付邊界

```powershell
go test ./modules/organization/...
go test -tags=integration ./tests/m3 -run 'TestA8[3-9]|TestA90|TestA91|TestA81Governance|TestGovernance|TestProvider'
```

真實 PostgreSQL 驗收涵蓋角色矩陣、雙 Org、撤權 admission／已 admission 快照、轉移、ownerless／復原、SQL／Audit／Outbox 故障原子性、獨立 replacement schema 匯入及既有 Envelope 對帳。模組已有實作與測試，M3 是否正式 Verified 仍由同 commit、兩平台累積 Gate／證據決定；完整背景 Job Worker 由 [platform/jobs](../../platform/jobs/README.md) 提供；本模組仍只提供中立 inventory 與 grant 契約，M4 已用真實持久化 Job 驗證替代 Provider 與當前 grant 撤銷。

Tenant 投影撤權由 optional migration 的 qualification hook 同交易鎖定受影響 Catalog 並更新 revision。最後有效 owner 消失時，Catalog／私有狀態持久標為 ownerless；Tenant 重新啟用、JoinAsOwner 或 Resume 都不會自動復活，只有具 tenant.scope.recover 的 Recover 重新建立有效 owner 並留下必要 Effects。雙 owner 同時撤權按 Catalog 鎖序列化。替換私有 schema 時須一併安裝同等 qualification hook，不能只搬三張主要表；Tenant 控制面原操作的必要稽核由可信同步入口負責。
