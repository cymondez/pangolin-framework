# Identity 與可擴充 Authentication

本套件定義與登入協定無關的 Credential、Verifier、Result、Pipeline，並擁有可信 identity 的私有 context 儲存。核心不解析 JWT、不讀 Cookie、不查 Session Store。

## 組裝與擴充

登入模組實作 `Verifier.Kind()` 與 `Verify(ctx, Credential) (Result, error)`，由可信組裝端交給 `NewPipeline(verifiers...)`。空、重複的 Kind 或缺少 Verifier 會被拒絕。AuthenticateCredential 每次只呼叫相符 Verifier，驗證失敗不改用其他來源重試。

Verifier 查驗真實憑證與當前登入狀態，不能信任請求中的 Principal DTO。完成時回傳 StatusAuthenticated、非空 Principal／Tenant／Method；Principal.Kind 為 user 或 service。Pipeline 驗證結果後才安裝身分，沒有公開的任意 identity setter。Session、API Key 或未來協定使用同一個 Port，無需修改 JWT 或核心；Authenticate 保留 Bearer 的便利入口。

Identity(ctx) 回傳 Principal、憑證攜帶的 Tenant hint 與安裝狀態；Method(ctx) 回傳非秘密的方式及 assurance，讀寫兩端都複製切片。context 不保存原始憑證。入口仍經 Tenant／Scope Resolver 的目前資格檢查，身分驗證不授予 Permission。

## 多步驟登入

未完成 MFA／step-up 回傳 StatusChallenge 與 Challenge；裝置等待核准可回傳 StatusPending。Pipeline 分別以 ChallengeError／PendingError 回報，支援 errors.Is／errors.As，均不回傳可繼續授權的 authenticated context。登入模組自行擁有 continuation 狀態、一次性驗證、重播防護與 login／redirect／polling endpoint，確認整個流程完成後才回傳 StatusAuthenticated。

目前交付擴充契約及 Adapter 接線；完整 MFA、OIDC、Device Login 協定需各自模組實作。

## 錯誤、取消與清理

空或過大的憑證、未知 Kind、非法結果回 ErrUnauthenticated；Verifier 錯誤保留 cause。驗證前後檢查取消，即使 Verifier 忽略取消，也不能替已取消操作安裝 identity。失敗時立即停止入口流程，不沿用驗證前的 context。

Pipeline 組裝後只讀註冊表，沒有背景 worker 或待關閉資源。Session Store、連線池、金鑰更新與清理由各 Verifier 模組擁有。

測試：`go test ./security/identity ./security/authn ./transport/http ./examples/bookstore/transport/http`。涵蓋分派、未完成狀態、取消及 metadata；跨入口仍需確認目前 Tenant／Scope／Permission，不能以安裝身分代替授權驗收。
