# JWT 與服務憑證 Adapter

本套件提供 RS256 JWT／JWKS Validator 及既有 ServiceVerifier 的相容 Adapter。通用 Identity、Verifier、完成／Challenge／Pending 結果與私有 context 儲存歸 [security/identity](../identity/README.md)；新增 Session、API Key 或其他登入模組不必修改 JWT 程式。

## JWT 組裝

```go
validator := authn.NewValidator(issuer, audience, jwksURL,
    &http.Client{Timeout: 2*time.Second})
authenticated, err := validator.Authenticate(ctx, bearerToken)
// 只有 err == nil 才將 authenticated 交給 Tenant／Scope Resolver。
```

Validator 實作 identity.Verifier，Kind 為 bearer；Verify 只回驗證結果，Authenticate／AuthenticateCredential 經共同 Pipeline 完成及安裝 identity。authn.Identity 與 authn.ErrUnauthenticated 保留相容轉接；新程式直接使用 identity 契約，不依賴 JWT Adapter 取得身分。

JWT 驗證 RS256、kid、RSA 簽章、issuer、字串／陣列 audience、exp、nbf，以及非空 sub／tenant_id。RSA modulus 至少 2048 bits，exponent 必須合法。Token 上限 16 KiB，JWKS 上限 64 KiB。可信 issuer／JWKS URL 來自啟動組態，不能採用請求指定的位置；正式部署使用 HTTPS 與有界 HTTP client。

每次驗證重新取得 JWKS；來源故障或不可驗證即拒絕。已驗證 Token 的非秘密 amr 可作 assurance metadata，不能代替應用的 step-up 政策。此 Adapter 不負責 OIDC discovery、登入 UI、協定回呼或 Session Store。

## 服務身分與其他登入方式

NewServiceAuthenticator(ServiceVerifier) 保留部署選定 mTLS／OAuth 憑證的入口，經共同 Pipeline 安裝 service identity。底層回傳其他 Principal.Kind、缺 ID／Tenant 或驗證失敗均拒絕。一般使用者 Session／API Key 模組直接實作 identity.Verifier，回傳 user identity，不能借用服務限定入口。

HTTP 使用共用 transport/http 的 CredentialExtractor 對接 Pipeline；預設 Bearer 仍可使用。Cookie 的安全屬性、Session 狀態及寫入時的 CSRF 防護由 Session 模組負責；見 [HTTP 模組](../../transport/http/README.md)。

## 驗證與清理

執行 go test ./security/authn ./security/identity。既有測試使用真實 RSA 簽章及 HTTP JWKS fixture，覆蓋偽造、過期、來源故障與輪替。登入後仍須驗證目前 Tenant／Scope／Permission。HTTP client 及外部資源由組裝端擁有；Adapter 不啟動背景程序，也不記錄 Token 或 JWKS 內容。
