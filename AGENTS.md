# panGOlin-framework 代理工作導覽

panGOlin-framework 是 Go 模組化應用框架，先以模組化單體交付，透過公開 Application Port 與版本化事件保留後續拆分服務的能力。核心設計是明確的依賴注入、以 `context.Context` 傳遞操作情境、嚴格的工作單元、Tenant／資源 Scope 隔離，以及可驗證的可靠事件配送。

本檔供開發代理理解專案與安排工作；詳細契約、驗收與排程由下列文件各自維護。文件與程式碼一律區分「規劃的行為」和「已有證據的實作」。

## 目前狀態與文件權威

**核對日期：2026-11-21。** M0 已 Verified，來源 commit `a0b1ad099fdf7bd28240514a24c34590220019a1`，見 [M0 驗收紀錄](docs/evidence/M0.md)。M1 已 Verified，來源 `9eb10df6cd848b87fda964f0b6b61d60dc12b0a6`，見 [M1 驗收紀錄](docs/evidence/M1.md)：交付 Kernel／DI／Config／Drain 與 tenant-only Bookstore Required 交易、JWT／RLS／分程序事件薄片，Windows／Linux（含 race）同版 Gate 通過，20 份模組 README 已補齊。完整 UOW／安全／通用 Adapter、CLI 與 Organization 仍依 M2–M6 交付。文件基線 v1.3、產品目標 v0.1；狀態由里程碑規劃書維護，重現方式見 [M1 開發指南](docs/guides/m1-development.md)。

本檔下文的 package、介面與執行流程都是既定設計的導覽，不表示對應目錄或功能已存在。實作開始後應同步更新這段狀態；不能由文件完成推論 Runtime 可執行或品質門檻已通過。

| 文件 | 唯一責任 | 何時閱讀 |
|---|---|---|
| [實作規範書](design/panGOlin-framework_實作規範書.md) | §5.7、§16 定義 95 項正式需求；相關章節定義介面、失敗與資料契約；§13 定義 A01–A95 與品質門檻；§15 定義版本範圍 | 實作、修改 API／schema、補測試、判斷產品行為前 |
| [里程碑規劃書](design/panGOlin-framework_里程碑規劃書.md) | M0–M6、工作包、前置 Gate、唯一需求交付映射及證據 | 拆工作、確認依賴、更新進度與驗收前 |
| [架構總覽](design/panGOlin-framework_完整架構與實作規劃書.md) | 系統定位、設計理由、關係圖與閱讀入口 | 首次理解專案或討論跨模組設計時 |
| [倉庫目錄指南](docs/guides/repository-layout.md) | 現行程式歸屬、範例與測試／工具入口 | 新增檔案、移動 package、尋找使用入口時 |

- 「應該怎麼運作」以實作規範為準；「目前實際怎麼運作」由程式碼與同版本的測試證據確認。兩者不一致時指出缺陷，不直接修改規範來合理化實作。
- 範例、圖表、本檔摘要與正式契約不一致時，修正說明性內容；正式契約有歧義時先釐清並修正唯一來源，不自行採較寬鬆的解讀。
- 需求語意變更先更新實作規範，再同步案例、交付映射與導覽。需求 ID 不因搬章節、拆 Issue 或延期而重編，也不重用已發布 ID。
- 外部技術文件用於查證實作細節；本專案的交易、安全與交付保證由本專案規範決定。

## 開工前檢查

1. 確認工作目錄、checkout／worktree 與分支，先讀 `git status --short`；保留既有未提交修改，不覆寫不屬於本次任務的內容。
2. 先讀實作規範 §1–§3，再依任務找到需求 ID、相關細則、§13 案例及里程碑規劃的工作包。不要只憑本檔摘要實作複雜機制。
3. 檢查實際檔案與工具配置。規範 §10 是目標目錄，§12 是待交付 CLI，不是現有檔案清單或已可執行的命令。
4. 確認工作前置條件。M0 已將 Go／PostgreSQL、module path、單一 Broker／Migration Tool 及本機配置選擇記錄於 `docs/adr/`、`deploy/toolchain.json` 與 `deploy/profiles/local.json`；公開 Port、fixture、估算及同版證據已通過 M0 Gate；Runtime 實作依 M1–M6 推進。不要將選型或示例當作 Runtime 已驗證。
5. 實作任務先建立對應驗收案例，再完成程式與回歸。尚未實作的案例保持明確待辦，不用空測試或假成功代替。

## 讀與改 Go 程式碼

- 先按擁有者歸類，再在擁有者內分層；不增加 `src`。框架 production package 不得 import `examples/`，Bookstore 專用 main／Handler／fixture 全部歸 `examples/bookstore`。單元測試與套件並置；跨套件驗收、共享 fixture、操作腳本與工具依 [目錄指南](docs/guides/repository-layout.md) 放置。

- 先用 `rg --files` 找檔案，`rg -n` 找定義、呼叫與配置，再閱讀相關 package、介面、建構函式及測試。工具可用時搭配 Go 語意工具確認引用與實作；文字搜尋沒有結果不等於不存在介面使用者。
- 依賴以 constructor 與明確 Port 傳入。package 使用小寫短名稱，公開介面按責任命名；import path 以實際 `go.mod` 為準，不自行杜撰發行位置。
- 情境與取消透過明確的 `context.Context` 傳遞。錯誤保留 cause，讓呼叫端可用 `errors.Is`／`errors.As` 判斷；清理錯誤不能蓋掉主要錯誤。
- 修改公開 Port 時檢查 Adapter、入口、測試與範本；修改 SQL 時同步 query source、schema／migration 與可重現的 sqlc 生成物，不直接手改生成檔。
- 只格式化本次修改的 Go 檔案。檢查指令以實際 workspace 與 CI 設定為準；不要宣稱執行了尚不存在的建置或測試流程。

## 基本運作原理

### 分層與跨模組互動

依據：[實作規範 §1](design/panGOlin-framework_實作規範書.md#s-1)、[§4](design/panGOlin-framework_實作規範書.md#s-4)。

- **Transport** 負責 HTTP／Worker 等入口、編解碼、驗證與錯誤映射，呼叫 Application，不操作業務資料表或具體 SQL。
- **Application** 負責 Use Case 編排、授權、DTO 與交易政策，依賴 Domain／共用 Port，不 import 具體 DB、Broker 或 HTTP Adapter。
- **Domain** 維護 Entity、Value Object、Aggregate 與業務不變條件，只依賴標準函式庫與極薄共用基元，不依賴 Runtime、HTTP、ORM 或外部服務。
- **Infrastructure** 實作 Domain／Application 定義的 Port；**Runtime／Platform** 透過抽象與已註冊 Provider 組裝，不認識業務模組特有模型。
- 跨模組只走公開 Application／Query Port、本機事件或版本化 Integration Event。不得直接操作另一模組的私有表或 package；跨 Aggregate 查詢用明確 Read Model。

### 模組、DI 與生命週期

依據：[實作規範 §2](design/panGOlin-framework_實作規範書.md#s-2)。

- Module 以編譯期明確註冊，宣告唯一 `Name`、`Version`、`DependsOn []Dependency` 及需要的 `RequiredScopeKinds`；不提供動態熱插拔。
- Runtime 先拒絕重名、缺漏相依、版本不相容與循環；穩定拓撲排序同層依名稱排序。
- 啟動順序是**全部模組的 PreConfigure → 全部 Configure → 全部 PostConfigure → 驗證並封存 Provider → 依序 Start → Ready**。不是逐模組一次跑完所有階段。
- Builder 階段只做註冊與組態，不解析尚未建好的 Runtime Service。Provider 重複、缺漏或循環在啟動前失敗；替換使用明確 API 並記錄原因，不以隱式覆寫處理。
- Singleton 僅保存可安全共用的狀態；Request／Operation Scope 的物件在自身範圍建構與清理，不讓 Singleton 保留請求情境。
- 啟動失敗回收已取得資源。正常停機先停止接入與 Worker 認領、Drain、有界取消／清理，再反序 Stop；DB／Broker 由其擁有模組最後關閉。每項資源只有一個關閉責任者。
- 啟動組態優先序是 `編譯預設 < 檔案 < 環境 < CLI`。必要設定缺漏或非法時拒絕啟動；Secrets 由環境或密鑰服務注入。

### 操作情境與入口

依據：[實作規範 §3](design/panGOlin-framework_實作規範書.md#s-3)、[§5.1](design/panGOlin-framework_實作規範書.md#s-5.1)、[§7.10](design/panGOlin-framework_實作規範書.md#s-7.10)。

- Principal、Tenant、Scope binding、Trace、UOW 經 `context.Context` 的受控存取與私有型別鍵傳遞；不建立全域 Current、goroutine-local、動態代理或反射攔截器。
- Tenant／Scope Hint 都是候選值。先驗證身分與授權，再由 Resolver 安裝不可變 binding；自行建立 struct 或在 DTO 填 owner 不會取得授權。
- 每個 HTTP、Job、Consumer 入口各自建立可信情境。Job／Event 保存 ScopeRef，執行時以目前服務身分重新授權，不重用已結束的 HTTP context；使用者代理型 Job 另驗該使用者目前資格。
- Repository 只使用 `Within` callback 傳入的 context；不保存舊 context、Tx 或 cursor 供範圍結束後使用。

一般非串流寫入順序：

`Trace／Timeout → Authenticate → Resolve Tenant → Resolve／Authorize Scope → Entry Policy → Decode／Validate → Within → Resource Policy（如需）→ Use Case → 完成交易 → 序列化 → 2xx`

只有確認 Commit 成功才能回傳寫入成功。提交後序列化／傳輸失敗仍可能已寫入資料，需保留操作識別與冪等查詢能力。Streaming／SSE／WebSocket 使用每次操作的短 Scope，不用一筆長交易包住連線。

### 工作單元與資料存取

依據：[實作規範 §5](design/panGOlin-framework_實作規範書.md#s-5)、[§8](design/panGOlin-framework_實作規範書.md#s-8)。修改前完整閱讀選項表、相容性矩陣、狀態與錯誤契約。

- 公開入口是 `UnitOfWorkManager.Within(ctx, options, callback)`；Manager 獨占完成、Commit、Rollback 與資源釋放責任。callback 返回 nil 只是要求完成，仍可能在完成階段失敗。
- Command 明示 ReadWrite；Query 明示 ReadOnly；直接呼叫未指定 Mode 預設 ReadOnly。ReadWrite 必須開交易；使用 transaction-local RLS 的讀取也必須開唯讀交易。
- `Required` 加入同一根 Scope，共用交易、連線、事件與完成責任；子範圍結束不提交。子失敗或選項不相容將根標成 rollback-only，外層捕捉錯誤也不能清除。
- 唯讀 Required 子視圖即使加入可寫根交易也不能寫入；其 context 在子 callback 結束即失效，根資源仍由外層管理。
- `RequiresNew` 暫停父 Scope、取得獨立資源，完成與清理後恢復父 Scope。子已提交不隨父回滾；它不是 Savepoint，也看不到父未提交資料。
- Required／RequiresNew 都不能切換 Tenant、Principal 或 ScopeRef。跨租戶／跨資源 Scope 工作另建重新授權的頂層操作；同根只允許一個 DB resource key，不承諾跨 DB 原子性。
- 沒有有效 Scope 時，業務 Repository 連讀取都拒絕。沒有可寫交易時拒絕寫入，不退回 Pool 自動提交；健康檢查與佇列候選枚舉使用專用 Infrastructure Port。
- 同一 UOW Executor 不允許平行 SQL；未關閉的 Rows／BatchResults 也算占用。Repository 負責關閉及檢查延遲錯誤，完成階段不能漏掉未釋放資源。
- `SaveChanges` 只 flush，不 Commit、不排空 Local Events、不觸發完成回呼。
- 正常完成：flush → 迭代排空 Local Events／flush → 同交易保存 Outbox、必要 Audit、啟用冪等時的成功結果 → 檢查資源／取消／rollback-only → 一次 Commit → 記錄 Outcome／釋放資源 → 回呼。
- Commit 結果無法確認時保存 `Unknown`，回傳可識別的 `UOW_COMMIT_UNKNOWN`；補做 Rollback 不能把它變成「確定未提交」。已確認 Committed 不因後續取消、回呼或傳輸失敗改寫。
- 清理使用獨立且有界的 CleanupTimeout；Panic 清理後重拋。完成回呼有 HookTimeout、沒有舊 UOW Executor，失敗不改寫已確認結果；可靠工作使用 Outbox。
- Manager 不自動重跑 callback。上層只有在確定回滾、Use Case 明示可重試且有界退避時，才以新根 Scope 重跑整個操作。
- 首版不提供非交易業務寫入、Savepoint、Suppress、跨 DB 原子提交或隱式重試；不以靜默降級補足未支援模式。

### Tenant、資源 Scope 與 Organization

依據：[實作規範 §7](design/panGOlin-framework_實作規範書.md#s-7)。

- Tenant 是最外層安全邊界；`ScopeRef = (TenantID, Kind, ID)` 表示單一 owner。tenant scope 的 ID 等於 TenantID；組織模組提供 `org.enterprise`、`org.organization`。
- 資料採 owner 精確隔離：Tenant、Enterprise、Organization Scope 各自只能存取自己擁有的業務資料；父層不是子層資料聯集。Enterprise 治理角色不自動取得 Organization 內容權限。
- Enterprise／Organization／Membership／治理放在**單一可選 `modules/organization` 模組**。Organization 可獨立，或隸屬同 Tenant 的一個 Enterprise；User 可加入多個 Organization，各自角色獨立。
- Core、UOW、一般 Repository、Events／Jobs 只依賴 `security/scope` 中立 Port，不 import 組織實作；組織管理透過 `modules/organization/contracts`。
- `tenant-only` 是預設組裝，不建立假 Organization；`organization-enabled` 同時加入完整模組與 Scope-aware schema。每種 Kind 僅有一個 Provider；缺必要能力拒絕啟動，授權來源故障時拒絕存取。
- 新頂層 UOW 在 callback 前重新確認權威 revision，防止 Bind 後撤權仍使用舊 allow。已 admission 的短 UOW 按快照完成；RequiresNew 保留父 binding 與期限，不藉此延長授權。
- Repository SQL 與 PostgreSQL RLS 都限制租戶／owner；RLS 使用 transaction-local 設定。應用 DB role 不得是 superuser、table owner 或有 BYPASSRLS，租戶業務表啟用 FORCE RLS。
- 明示區分 TenantOnly／ScopeAware 表。ScopeAware 業務表參照中立 `resource_scopes` catalog，唯一鍵與同 owner 外鍵含完整 owner；不綁死組織模組私有表。
- 啟用組織 schema 時將既有資料明確回填為 `tenant/TenantID`，原子替換 RLS policy，防止留下 permissive policy 被 OR 放寬。
- Provider 替換保留 Scope ID、membership／role／state／revision 與公開契約；已有非 Tenant 資料或未完成工作時不能直接移除模組、降級為 tenant-only。
- Permission 預設拒絕，User 明確 deny 優先；Scope membership、Permission、Resource Policy 與上層限制共同判斷。Settings／Feature 不授予權限；Host 跨租戶操作走獨立且受稽核的授權路徑。

### 事件、背景工作與冪等

依據：[實作規範 §6](design/panGOlin-framework_實作規範書.md#s-6)、[§9.4](design/panGOlin-framework_實作規範書.md#s-9.4)、[§14.2](design/panGOlin-framework_實作規範書.md#s-14.2)。

- Local Event 在目前 UOW 排隊，Handler 依 Priority、註冊順序執行，新事件按 FIFO 迭代；超過配置數量／深度或處理失敗使交易失敗。
- 可靠 Integration Event 在目前可寫根交易寫 Outbox；缺少條件就拒絕，不能退化成直接 Broker Send。Direct send 沒有與業務 DB 原子一致的保證。
- Outbox 用短交易認領，每次產生唯一 `lease_token`，交易外送 Broker；只有持久化與路由確認符合契約才標 sent，所有狀態更新都驗 token。
- Inbox 先驗證並持久化完整 Envelope 後 ACK，依 `(consumer_group, event_id)` 去重；同 ID 不同內容隔離告警。處理端在**同一交易持有行鎖**，完成業務修改與 processed 狀態，不套用 Outbox 的放鎖後配送流程。
- 配送是 at-least-once；外部付款、郵件等副作用需穩定冪等鍵或另由 Outbox 派送，不宣稱端到端 exactly-once。
- Event／Job 保存完整 Tenant／ScopeRef，執行時重新授權；重送保留 Event ID、owner 與版本化 Envelope。Job 使用持久化狀態、租約 token、有界重試與 dead-letter。
- Command 冪等唯一鍵包含 Tenant、Scope Kind／ID、Principal、operation、key；在業務修改前保留，與成功結果／Outbox 同交易提交。併發靠 DB 唯一約束，不以先查後寫取代。
- Commit Unknown 用原 key 對帳；查無結果不代表確定失敗。結果查詢與重播重新驗證目前權限，不能藉快取或舊成功紀錄繞過撤權。

## 機制與模組索引

以下是目標 package 與權威章節；建檔前先確認目前實際結構。

| 工作／需求 | 設計位置 | 細則入口 |
|---|---|---|
| 模組 DAG、DI、組態、啟停 | `kernel/module`、`kernel/di`、`kernel/config`、`kernel/runtime` | 實作規範 §2、§14.3 |
| Use Case、Domain、HTTP 與錯誤 | `application`、`domain`、`transport/http`、`kernel/errors` | §1、§3、§4、§5.6 |
| 工作單元、Repository、SQL、遷移 | `persistence/uow`、`persistence/postgres`、`persistence/migration` | §5、§8 |
| 身分、Permission、Tenant、Scope | `security/*`、`tenancy` | §7.1–§7.5、§7.7、§7.9–§7.10 |
| Enterprise／Organization／Membership | `modules/organization` | §7.6–§7.12 |
| Local／Integration Event、Outbox／Inbox | `events/local`、`events/distributed`、`events/outbox`、`events/inbox` | §6 |
| 必要稽核與安全事件 | `platform/audit` | §9.1；必要稽核同業務交易，失敗安全事件走獨立日誌 |
| Settings／Features | `platform/settings`、`platform/features` | §9.2、§7.10；分層解析、版本失效與 Permission 優先 |
| 快取 | `platform/cache` | §9.3；key 含 application／version／tenant／完整 scope，權限相關結果另含 principal／revision |
| 背景工作 | `platform/jobs` | §9.4、§14.3 |
| Logs／Metrics／Tracing／Health | `observability` | §9.6；敏感內容遮罩，不以無界租戶／組織 labels 建 Metrics |
| CLI、兩種 Profile 範本與 Bookstore | `cmd/pangolin`（後續 CLI）、`examples/bookstore/cmd/{api,worker}`、`examples/bookstore` | §10、§12、§13.4 |

v0.1 的正式範圍只由[實作規範 §15](design/panGOlin-framework_實作規範書.md#s-15)決定。Organization 對應用可選，但仍是框架發行包交付項。Redis、Blob Storage、進階 Cron、新的 DB／Broker／Transport Adapter 列在 v0.2+，沒有本次時程承諾。

## 常見開發工作

**建立模組或擴充公開 Port**

- 先確認需求與交付工作包，定義模組責任、公開 Port、直接依賴、Scope capability 與適用 Profile。
- 宣告 Module、Provider 與分階段 lifecycle；補齊重名／缺依賴／循環／啟動失敗清理等相關案例。不要為了註冊方便反轉依賴或引入組織核心耦合。
- 同步 Adapter、公開契約測試、範例、指南與必要 ADR；新的正式需求先加入實作規範與唯一交付映射，不在本檔另創需求。

**新增 Aggregate、Repository 或 schema**

- 業務不變條件放 Domain，Port 僅暴露業務需要的操作；查詢明示排序、分頁與上限，不預設萬用 CRUD／ListAll。
- 宣告 TenantOnly 或 ScopeAware，設計租戶／owner 唯一鍵、複合外鍵、RLS、版本比較與軟刪除查詢條件；一般程式不能自由停用隔離。
- pgx/sqlc Adapter 從當前 UOW 取得受控 Executor，不能自行 Begin 或用 Pool 繞過交易。
- Migration 由單一選定工具在部署階段依模組相依排序並持有全域執行鎖；API 啟動只檢查相容性。遷移優先向前修復，評估資料影響後才決定復原方式。

**新增 HTTP、Job、Consumer 或事件契約**

- 入口明確選擇 Command／Query Policy、驗證 Principal／Tenant／Scope，套用授權、大小限制與 deadline；敏感 Use Case 保留自身授權檢查。
- HTTP 錯誤遵守 Problem Details 與規範錯誤碼，不暴露 SQL、堆疊或其他租戶存在性。
- Event Envelope 透過 Serializer Registry 定義 snake_case wire 欄位及 schema version；保護已發布版本，必要時提供 Upcaster。
- 測試成功之外，也驗證取消、重送、授權失效與提交前後故障；無法取得確認時保留 Unknown，不宣告回滾成功。

## 驗證與交付

依據：[實作規範 §13](design/panGOlin-framework_實作規範書.md#s-13)、[里程碑規劃書](design/panGOlin-framework_里程碑規劃書.md)。

目前完整入口為 M1；Windows 使用 `scripts/check.ps1`，Linux 使用 `scripts/check.sh`，依 [M1 開發指南](docs/guides/m1-development.md) 設定連線並先盤點共用環境。`m0.py check` 保留契約盤點，M0 歷史 verify 不代替 M1 Gate。文件修改仍需檢查連結、章節、ID 與敘述一致性；品質工具測試不能代替 Runtime 驗收。從實際 module／workspace 執行適用的基礎檢查：

```powershell
go env GOMOD GOWORK
go build ./...
go test ./...
go vet ./...
go test -race ./...
```

以上不是完整發布 Gate；CI、build tags、支援平台與真實服務配置固定後，以其可重現流程為準。

- 修改對應 A01–A95 的適用子情境都要驗證；正式增量交付另跑所有先前已交付案例的累積回歸。必跑案例 skip 等同失敗，N/A 需符合 Profile 邊界並附理由。
- PostgreSQL 交易／RLS／提交結果與 Broker 配送使用固定版本真實服務；Mock 不能作為上述語意的完成證據。併發以 barrier 控制時序，資料一致性用提交後獨立連線對帳。
- Framework 發布矩陣包含 tenant-only、organization-enabled、replacement contract；替代 Provider 需通過相同契約，不以可編譯代替驗收。
- Go statement coverage 的整體手寫 production code 及每個規範列出的關鍵 package 各至少 90.00%，以原始計數判定，不四捨五入、不平均百分比。未測 package 仍入分母；排除項需逐檔 manifest。
- Unit／Integration／Contract 與分程序 E2E 的 coverage 依 §13.3 收集，合併相同 source／build metadata 的 statement block 聯集；另通過 race、靜態分析、license／依賴檢查。
- 證據綁定同一 commit、規範基線、Profile、依賴版本、配置與 seed，包含命令／結果及必要對帳。未實作或環境受阻時明確報告限制，不把文件檢查當 runtime 證據。
- 狀態依 `Planned → Ready → In progress → Evidence ready → Verified` 維護；只有同 commit 的 Exit 證據與 Gate 通過才標 Verified。排程、工作包與交付責任只更新里程碑規劃書。

## 本機 Windows 工具

- Shell 工具呼叫使用 `login=false`，避免個人 PowerShell profile 的存取錯誤。
- 回報工具缺少前先用 `Get-Command -All <工具名>` 檢查解析位置，再執行版本檢查。區分找不到命令、程序無法啟動、套件錯誤與外部服務存取失敗。
- Python 使用 `python` 或 `py`，安裝套件用選定 interpreter 的 `python -m pip`；不要依賴 `python3` 的 WindowsApps alias。
- Node 使用 `node`，npm／npx 優先使用 `npm.cmd`／`npx.cmd`。遵守專案指定版本與虛擬環境。
- PATH 不完整時可查實際安裝位置：Python 為 `C:\Python313\python.exe` 或 `C:\Users\cymondez\AppData\Local\Programs\Python\Python312\python.exe`；Node/npm/npx 為 `C:\Users\cymondez\AppData\Local\Author Software\nvm\.nodejs`；Docker 為 `C:\Users\cymondez\AppData\Local\Programs\DockerDesktop\resources\bin`。
- Docker／Compose 已安裝；連接 `npipe:////./pipe/dockerDesktopLinuxEngine` 遭拒是 engine 權限問題，不是未安裝。僅針對已授權工作的實際存取需求走正常核准流程。
- 絕對 executable path 只選擇安裝位置，不增加 sandbox 權限；不要為了 PATH 或命令名稱問題修改核准規則。

## 維護導覽

- **模組實作必須附說明文件。** 新增或變更模組／production package 時，在該目錄維護 `README.md`，說明責任、公開 API／組裝與設定、錯誤及清理行為、測試方式與目前交付邊界；同一變更完成，不能只更新根目錄摘要。M1 文件索引由 `tests/contract/m1.json` 與 Gate 檢查。

- 新增或修改模組、依賴、公開 Port、入口政策、交易／安全機制時，同一變更更新相應文件與本檔索引；刪除或改名也同步修正。
- 本檔保持摘要與導覽用途。產品規範留在實作規範書，交付映射留在里程碑規劃書，不複製另一份需求清單或排程。
- 有程式碼與證據後更新「目前狀態」；新增詳細指南時才加入實際檔案連結，不宣稱不存在的規則檔會自動載入。
- 現行指南描述目前有效設計，升級差異放 Release Notes／Migration Plan；ADR 保留決策理由與歷史。v0.1 API 變更仍需變更與遷移說明，已發布 Event Schema 不可無通知破壞。
- 最終回報說明修改內容、驗證結果與未完成限制；不把規劃、片段編譯或工具可用性寫成產品交付完成。
