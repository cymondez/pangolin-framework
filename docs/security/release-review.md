# v0.1.0 安全審查（Release Security Review）

本文件是 panGOlin-framework v0.1.0 發布前的正式安全審查，依[實作規範 §14.1](../../design/panGOlin-framework_實作規範書.md#s-14.1)對每個威脅類別記錄攻擊面、濫用路徑、控制措施、可執行測試與剩餘風險。`scripts/release_drills.py` 的 security-review 演練解析本文件的控制清單、逐一執行所列測試，並在出現未解決的高風險項時阻擋發布（A72／REL-005）。

審查基線：規範 v1.3；測試證據以同 commit 的累積 Gate 為準。本文件描述的是已實作並由測試覆蓋的控制；尚未實作的緩解不會寫進 control 欄位。所有測試選擇器（`package:Test`）指向實際存在的可執行驗收測試，由演練逐一執行並驗證通過、無 skip。

```json
[
  {"category":"identity-impersonation","attack_surface":"HTTP/Job/Consumer 入口的 Bearer JWT 憑證與 IdP 簽章金鑰輪替","abuse_path":"偽造、過期、錯誤簽章/演算法/issuer/audience 的 Token 冒用其他 Principal，或以 service 簽章鑄造 user 身分","control":"authn 驗證 RS256 簽章、issuer、audience、效期與 JWKS 輪替；身分僅由 trusted identity pipeline 安裝，service adapter 不得鑄造 user principal","test":"security/authn:TestJWTSignatureClaimsAndRotation","residual_risk":"low：測試 IdP 僅為 fixture；正式環境須由部署方提供可信 IdP 與金鑰輪替政策"},
  {"category":"tenant-header-forgery","attack_surface":"X-Tenant-ID／X-Scope 等租戶與範圍提示欄位","abuse_path":"夾帶他人租戶或 scope 提示嘗試越權繫結","control":"提示僅為候選值；Resolver 驗證身分後才建立不可變 Tenant/Scope binding，衝突回 PERMISSION_DENIED，未支援 kind 回 SCOPE_KIND_UNSUPPORTED，未通過前不開業務交易","test":"examples/bookstore/transport/http:TestExternalUserSessionReachesBookstore;transport/http:TestA48Pipeline","residual_risk":"low"},
  {"category":"sql-http-injection","attack_surface":"進入 SQL 的 DTO 欄位、HTTP 路徑與請求內容","abuse_path":"以注入語法修改查詢語意、破壞 schema 或透過超大 body 干擾入口","control":"Repository 全部使用 sqlc 參數化查詢；DTO/入口驗證與大小上限在交易前拒絕非法輸入；注入 payload 以資料儲存、絕不執行","test":"tests/m6:TestM6SecurityDrill;transport/http:TestA48Pipeline","residual_risk":"low：應用端仍須保持參數化慣例，框架不攔截繞過 Repository 的手寫拼接 SQL"},
  {"category":"cross-scope-idor","attack_surface":"不同租戶／Organization／Enterprise 擁有相同業務 ID 的資料列","abuse_path":"跨租戶或跨 owner 的讀寫、缺少 owner 條件的 SQL、父層 scope 讀取子層內容","control":"SQL 恆帶 exact-owner 述詞，FORCE RLS 以 transaction-local 設定做資料庫層第二道防線；中立 resource_scopes catalog 以複合外鍵擋跨 owner 引用；父層 scope 不是子層聯集","test":"tests/m3:TestA56TenantRLS;tests/m3:TestA77ScopeIsolation;tests/m6:TestM6SecurityDrill","residual_risk":"low"},
  {"category":"governance-role-impersonation","attack_surface":"Enterprise／治理角色與內容權限的交界","abuse_path":"Enterprise owner 或推導成員直接讀取 Organization 內容、self-elevation、父層 allow 建立子層 membership","control":"治理角色只取得治理資料的授權；內容權限仍需明確 membership；explicit deny 優先；加入 owner 需治理權限並留 Audit；Feature 開啟不授予權限","test":"tests/m3:TestA85GovernanceBoundary;tests/m3:TestA86RolePolicy","residual_risk":"low"},
  {"category":"disabled-module-owner-downgrade","attack_surface":"移除或停用 organization Provider／模組時的既有非 Tenant 資料與 pending 工作","abuse_path":"降級成 tenant-only 使 Organization 資料變成租戶可見、政策被 OR 放寬或資料被丟棄","control":"缺少必要 capability 或存在非 Tenant catalog／pending Job／Event 時啟動直接拒絕（不 Ready）；不改寫 owner、不自動 drop 模組表、不退回 tenant 例外路徑","test":"tests/m3:TestA92Downgrade","residual_risk":"none"},
  {"category":"duplicate-event-double-charge","attack_surface":"at-least-once 事件配送與 Command 重送的重複執行","abuse_path":"同一事件被重複處理造成二次副作用；同鍵 Command 重送執行第二次業務修改","control":"Inbox 在同一交易持有行鎖並以 (consumer_group,event_id) 去重；Command 冪等鍵在業務修改前以唯一約束保留並重放已保存結果；外部副作用要求穩定冪等鍵或由本地 Outbox 派送，不宣稱端到端 exactly-once","test":"tests/m3:TestA06InboxLock;tests/m3:TestA07ResultReplay;tests/m3:TestA54ExternalIdempotency","residual_risk":"low：配送契約為 at-least-once；不具冪等能力的外部端點不被標示為不重複保證"},
  {"category":"worker-retry-storm","attack_surface":"Outbox/Inbox/Jobs Worker 的租約、重試與容量","abuse_path":"過期 Worker 以舊 token 覆寫狀態、無界重試或積壓拖垮系統","control":"唯一 lease_token 欄柵使舊 Worker 更新零列；重試有界退避與 dead-letter；容量以 advisory admission 鎖與上限拒絕超限接單，不靜默丟棄","test":"tests/m3:TestA11LeaseFencing;tests/m4:TestJobStoreBoundaries;tests/m4:TestEventCapacity","residual_risk":"low：超限政策須寫入部署 profile（見 operations runbook）"},
  {"category":"unauthorized-host-exception","attack_surface":"Host 管理身分的跨租戶操作路徑","abuse_path":"一般身分以空 Tenant 或 Filter Override 取得全租戶視野","control":"Host 例外是獨立、明確授權且全程稽核的路徑；一般身分與一般連線一律拒絕 override，沒有全域 bypass","test":"security/policy:TestA58Host","residual_risk":"none"},
  {"category":"secret-leak","attack_surface":"錯誤回應、logs、traces、metrics 與稽核輸出","abuse_path":"從錯誤 detail、堆疊或觀測資料取得 DSN、SQL、Token 或其他租戶存在性","control":"UOW 以 sentinel code 分類且 detail 不含 DSN/SQL/Token；HTTP 依 problem+json 映射遮罩；telemetry 對敏感內容遮罩，metrics 不使用租戶／組織 labels","test":"persistence/uow:TestM2ViewGuards;transport/http:TestA42HTTPMapping;observability:TestA66Observability","residual_risk":"low"},
  {"category":"cache-poisoning","attack_surface":"跨 scope 共用的 Cache／Settings／Features 索引鍵與失效","abuse_path":"以一個 scope 的快取或失效影響另一個 scope 的結果，或藉 Feature 開啟取得權限","control":"快取鍵含 application/version/tenant/完整 scope，權限相關結果另含 principal/revision；填值與失效皆 scope 隔離；Settings 分層優先序固定、版本失效收斂；Feature 永不授予權限","test":"platform/cache:TestA65Cache;platform/settings:TestA65Settings;platform/features:TestA65Features","residual_risk":"low"},
  {"category":"excessive-database-privilege","attack_surface":"資料庫角色、物件擁有權與 RLS 強制","abuse_path":"應用角色以 superuser/BYPASSRLS 繞過隔離、自行遷移或擁有業務表","control":"fixture 採 NOLOGIN owner 角色與分離的 migrator；app/worker NOBYPASSRLS/NOSUPERUSER 且僅有最小表格授權，不可建立角色／資料庫；所有 owner-scoped 表 FORCE RLS；業務表不為 app/worker 擁有","test":"tests/m6:TestM6SecurityDrill;tests/m3:TestA56TenantRLS","residual_risk":"low：見下方 tenant_memberships 審查紀錄"}
]
```

## 各類別審查說明

- **identity-impersonation（身分冒用）**：JWT 驗證覆蓋合法、過期、偽造與新舊簽章 key 輪替（A12）；`security/identity` 的 pipeline 是唯一能安裝身分的路徑，HTTP body 與 DTO 無法安裝 Principal。Bookstore HTTP 層的 forged-token 由 `TestM6CleanRebuild` 演練實測回 401。
- **tenant-header-forgery（Tenant Header 偽造）**：租戶提示在 Resolver 驗證前只是候選值；範例入口的 tenant-hint/scope-id 偽造與跨租戶 session 都有專屬負測試（`TestExternalUserSessionReachesBookstore`），重建演練也對 `X-Tenant-ID` 偽造實測回 403。
- **sql-http-injection（SQL/HTTP 注入）**：框架端所有經 sqlc 生成的查詢皆參數化；`TestM6SecurityDrill` 以含 `DROP TABLE`、引號、反斜線與多位元組字元的 payload 走真實 Application/UOW 路徑，證明逐字儲存、表結構完好且 Outbox 事件照常落庫；HTTP 層的 malformed DTO、oversized body 由 `TestA48Pipeline` 驗證在交易前拒絕。
- **cross-scope-idor（跨租戶／跨 Organization／跨 Enterprise IDOR）**：資料層以 exact-owner 述詞 + FORCE RLS 雙重防線；升級與安全演練實測缺少 owner 條件的 SQL、跨租戶寫入／軟刪除與 tenant/org 互讀全部為零列。中立 `resource_scopes` catalog 的複合外鍵禁止跨 owner 引用（A78 另有專測）。
- **governance-role-impersonation（治理角色冒充內容權限）**：Enterprise owner 無 Org membership 時不可讀 Org 內容、推導 membership 不產生權限、self-elevation 與父層 allow 穿透都有負測試（A85/A86）。
- **disabled-module-owner-downgrade（停用模組導致 owner 降級）**：A92 證明缺 capability、非 Tenant catalog 或 pending 工作時啟動直接拒絕；升級演練另證明 policy 切換不會留下寬鬆 policy union。
- **duplicate-event-double-charge（重複事件導致重複扣款）**：配送契約是 at-least-once；Inbox 行鎖與冪等鍵重放（A06/A07/A54）是防線，外部端點必須自備穩定冪等鍵——這是文件化的契約界線，不是框架可單方面消除的風險。
- **worker-retry-storm（Worker 重試風暴）**：lease 欄柵（A11）與有界重試／dead-letter／容量 admission（M4）共同限制重試放大；[operations runbook](../runbooks/operations.md) 的佇列訊號表定義超限處置。
- **unauthorized-host-exception（未授權的 Host 例外路徑）**：Host 路徑獨立、授權且可稽核；一般連線沒有全域 bypass（A58）。
- **secret-leak（Secrets 泄漏）**：錯誤分類與 problem+json 遮罩（A42/A14）、telemetry 遮罩（A66）與 A62 的存在性遮蔽共同覆蓋；DSN/Token/SQL 不出現在公開 detail。
- **cache-poisoning（Cache Poisoning）**：快取鍵含完整 scope 與 principal/revision，失效與填值皆不跨 scope（A65）；Settings 分層與版本收斂防繼承濫用；Feature 不授權。
- **excessive-database-privilege（資料庫特權過大）**：角色姿態（NOBYPASSRLS、最小授權、不可建角色／資料庫）、物件擁有權與 FORCE RLS 不變量在 `TestM6SecurityDrill` 逐表驗證；遷移 role 與 app role 分離另由 M3 遷移邊界測試證明（app role 無法遷移）。

## 審查紀錄與已接受事項

- **`pangolin_security.tenant_memberships` 無 RLS**（審查結果，已接受）：此表由 tenancy 遷移（3000001）建立，供 trusted tenant authority 讀取成員資格投影；fixture 授權 app/worker 角色 SELECT 以便 authority 查詢。它是權威中繼資料（tenant、principal 種類、狀態、revision），不含業務內容；業務資料隔離不受影響。剩餘風險評估為 low：持有 app/worker 憑證的攻擊者可跨租戶讀取成員資格中繼資料，屬部署層的憑證保護責任；後續版本可考慮將 authority 讀取改走 `pangolin_scope_authority` 專屬角色並收緊 fixture 授權。此紀錄不改動既有契約。
- **at-least-once 配送**：框架保證可靠配送與冪等基礎設施；對外部系統的重複副作用由消費端冪等鍵控制，發布文件明示此界線。
- **fixture 憑證**：`deploy/compose` 內的帳號密碼僅供拋棄式測試環境；正式部署的 Secrets 由環境或密鑰服務注入（§14.3），不得沿用。

## 發布判定

security-review 演練對上述控制清單逐項執行測試並驗證無 skip；任何類別出現未解決的高風險項時，`scripts/release_drills.py` 依 fail-closed 原則讓發布演練失敗。本審查在 v0.1.0 範圍內沒有未解決的高風險項；low 等剩餘風險均屬部署方責任並已記錄於各控制項。
