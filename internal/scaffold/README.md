# pangolin CLI implementation

CLI.Run 接受明確工作目錄、context 與輸出 writer，失敗回非零。new、module add、migrate create/status、doctor、generate openapi 均提供 help。名稱限小寫 Go package 形式；new 在同目錄 staging 後發布，已存在目的地拒絕。重複 module／migration 拒絕；OpenAPI 同內容重入成功，使用者修改的檔案保留。

assets/common 與兩種 Profile 包含完整範本。scripts/template_bundle.py 從實際 production packages、go.mod/sum、VERSION 與 local IdP 再生成 runtime 資產，--check 阻擋來源漂移。純文字模板不在 framework Go coverage 分母；CLI 包裝仍完整量測，生成物另建置並跑真實 SQL／HTTP 契約。tenant-only 不輸出 organization implementation；organization-enabled 一次輸出完整模組。

v0.1.0 尚未發行時用 bundled runtime 的本機 replace，版本與 SHA-256 manifest 固定；不依賴不存在的遠端 tag，也不從網路下載範本。migrate status 僅查 Goose ledger，DSN 從環境注入且不回顯。部署 migrate 在生成專案內執行，API 只檢查版本。doctor 檢查 Go、Compose 與 runtime。

沒有常駐資源。CLI 擁有自己的 sql.DB，status 返回時關閉；呼叫者擁有 context／輸出。驗證：go test ./internal/scaffold、python scripts/template_bundle.py --check；完整生成物驗收由 scripts/m5_templates.py 執行。

生成 API 將實際 Scope Registry 註冊進 Runtime DI，tenant／完整組織模組宣告 RequiredScopeKinds，封存時檢查能力。資料庫 profile marker 在 Ready 前核對；組織 DB 不能直接接到 tenant-only 生成物。生成契約另用獨立管理連線核對 book／Audit／Outbox／冪等結果數、FORCE RLS 與應用角色權限。

專案名稱支援小寫字母、數字、底線及連字號（例如規範的 bookstore-org）；模組名稱使用合法 Go package 識別字。生成物驗收直接使用規範的 bookstore／bookstore-org 命令，實際建置含連字號的 module import path。

兩種 Profile 的 Tenant Resolver 使用 security/identity 共同 reader，HTTP／組織控制面使用 AuthenticateCredential／CredentialExtractor Port。預設組裝選 JWT／Bearer；新增登入模組替換應用的可信 Verifier／擷取組裝，不改框架核心。Runtime bundle 同步包含中立 Identity 與各資料存取 Adapter；引擎專用完整 Profile 的支援邊界維持明示，不因 bundle 包含 Driver 就宣稱全部 SQL 已跨引擎移植。
