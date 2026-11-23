# Authentication：M1 RS256 JWT 驗證

此套件從可信 JWKS URL 驗證 JWT，成功後才在 private context key 安裝 identity。`Identity(ctx)` 供 Tenant／Scope Resolver 讀取 Principal 與 Tenant；不提供可任意安裝 Principal 的 setter。實作見 [jwt.go](jwt.go)。

```go
validator := authn.NewValidator(issuer, audience, jwksURL,
    &http.Client{Timeout: 2*time.Second})
authenticated, err := validator.Authenticate(ctx, bearerToken)
// err 非 nil 即拒絕；只將 authenticated 交給後續 Resolver。
```

M1 只接受 RS256，驗證演算法、kid、RSA 簽章、issuer、字串／陣列 audience、exp、nbf，以及非空 sub／tenant_id。RSA modulus 至少 2048 bits，公鑰 exponent 需合法。Token 上限 16 KiB，JWKS 上限 64 KiB；HTTP deadline 與 client timeout 都必須有界。

每次驗證重新取得可信 JWKS，因此新公鑰可在輪替後使用；不可驗證、來源故障與非法 Token 一律 `ErrUnauthenticated`，不回傳 Token 或 JWKS 內容。Issuer 與 JWKS URL 必須來自可信啟動組態，不能採用請求內的 URL。M1 的 HTTP issuer 僅用於 loopback／隔離測試；正式部署應使用 HTTPS。

這是 M1 身分切片：完整 OIDC discovery、快取／更新策略、多演算法、撤權與授權矩陣仍由 M3 交付。身分驗證成功不等於擁有某個資源 Scope 的權限；後續仍需 Resolver／Application 授權。

驗證：`go test ./security/authn`，涵蓋真實 RSA 簽章、claim 邊界、偽造、過期、JWKS 故障與輪替。分程序真實測試 IdP 見 [M1 指南](../../docs/guides/m1-development.md)。

服務入口可透過 `NewServiceAuthenticator(ServiceVerifier)` 安裝私有 service identity；Verifier 必須驗證部署選定的 mTLS／OAuth credential，回傳目前 Tenant 資格再交 tenancy／Scope Provider 查驗。服務身分不是全域管理員，缺 service Kind／ID／Tenant 或驗證失敗即 UNAUTHENTICATED。
