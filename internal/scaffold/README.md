# pangolin CLI implementation

`new --realtime` 可將 SignalR 加入任一 Profile。啟用時生成 `REALTIME.md` 及 Root 模組裝配，`module add --depends realtime` 對應可注入 `realtime.Publisher` 的直接相依。

CLI.Run 接受明確工作目錄、context 與輸出 writer，失敗回非零。new、module add、migrate create/status、doctor、generate openapi 均提供 help。名稱限小寫 Go package 形式；new 在同目錄 staging 後發布，已存在目的地拒絕。重複 module／migration 拒絕；OpenAPI 同內容重入成功，使用者修改的檔案保留。

2026-11-25 起「Root 裝配與建構子 DI」增補改變兩個生成面：`module add` 產出 Root 模式 module.go（`ModuleMetadata`／`DependsOn() []module.Ref`／`RootBuilder` hooks），並登記進 `modules/registry.go` 產出的 `UserRoot` Type 聚合（菱形去重）；`foundation` 套件的 `foundation.Module` 平台模組擁有全部 DB／Broker／事件資源與公開框架契約，`foundation.Profile` 模組只在同一契約擁有者下替換 Profile 實作，`internal/app` 根模組僅保留 HTTP／drain 組裝與 deployment 相容檢查（invalid graph 不取用任何資源，cleanup 由 Container 單一擁有）。`module add --depends persistence/authorization`（或 `organization`）翻譯為 `module.Of[foundation.Module]()` Type 邊並在 generated module 中示範 typed constructor injection（`uow.UnitOfWorkManager`、`foundation.Scopes`、`foundation.Publisher`、`foundation.Idempotency`）；`bookstore` 是生成網域示例名稱，拒絕作為 user module 依賴；pangolin.json 的 framework 名稱保留給 migration 排序。

2026-11-26 冪等授權修正：`foundation.Idempotency` 的操作授權回呼不再返回常量拒絕。tenant-only 由 `newIdempotency(foundation.ScopeAccess)` 依賴 Scopes 契約，pending／replay 以 `scopes.Authorize(c, operation)` 對已 realize 的 registry 重新授權；organization-enabled 由單一私有 `organizationRuntime` bundle 工廠一次組裝 organization Store、registry 與 idempotency（注入僅 `AuthorityDB`／`UnitOfWorkManager`／`Effects`，bundle 不依賴 Scopes／Idempotency 契約，組合無 constructor cycle；idempotency 回呼 closure 在工廠返回前完成 registry 初始化，未初始化一律 fail closed），再以 projection 取代 Idempotency 實作並 ReplaceContract Scopes——公開契約擁有者仍是 foundation，組織 Store、idempotency replay 與 HTTP 控制面使用同一 registry／idempotency 實例，不改寫任何 permission／replay／revision 檢查。

同日 UOW pool 修正：`newWorkUnits` 以 application `DB` handle 組裝 UOW Manager——業務表 grant 屬應用角色，業務交易必須走 ApplicationDatabase；`AuthorityDB`（worker 角色）僅用於 tenant／event／scope metadata（newEventPublisher／newTenants／newScopes），不調整任何 SQL grant。

assets/common 與兩種 Profile 包含完整範本。scripts/template_bundle.py 從實際 production packages、go.mod/sum、VERSION 與 local IdP 再生成 runtime 資產，--check 阻擋來源漂移。純文字模板不在 framework Go coverage 分母；CLI 包裝仍完整量測，生成物另建置並跑真實 SQL／HTTP 契約。tenant-only 不輸出 organization implementation；organization-enabled 一次輸出完整模組。

v0.1.0 尚未發行時用 bundled runtime 的本機 replace，版本與 SHA-256 manifest 固定；不依賴不存在的遠端 tag，也不從網路下載範本。migrate status 僅查 Goose ledger，DSN 從環境注入且不回顯。部署 migrate 在生成專案內執行，API 只檢查版本。doctor 檢查 Go、Compose 與 runtime。

沒有常駐資源。CLI 擁有自己的 sql.DB，status 返回時關閉；呼叫者擁有 context／輸出。驗證：go test ./internal/scaffold、python scripts/template_bundle.py --check；完整生成物驗收由 scripts/m5_templates.py 執行。

生成 API 將實際 Scope Registry 註冊進 Runtime DI，tenant／完整組織模組宣告 RequiredScopeKinds，封存時檢查能力。資料庫 profile marker 在 Ready 前核對；組織 DB 不能直接接到 tenant-only 生成物。生成契約另用獨立管理連線核對 book／Audit／Outbox／冪等結果數、FORCE RLS 與應用角色權限。

專案名稱支援小寫字母、數字、底線及連字號（例如規範的 bookstore-org）；模組名稱使用合法 Go package 識別字。生成物驗收直接使用規範的 bookstore／bookstore-org 命令，實際建置含連字號的 module import path。

兩種 Profile 的 Tenant Resolver 使用 security/identity 共同 reader，HTTP／組織控制面使用 AuthenticateCredential／CredentialExtractor Port。預設組裝選 JWT／Bearer；新增登入模組替換應用的可信 Verifier／擷取組裝，不改框架核心。Runtime bundle 同步包含中立 Identity 與各資料存取 Adapter；引擎專用完整 Profile 的支援邊界維持明示，不因 bundle 包含 Driver 就宣稱全部 SQL 已跨引擎移植。
