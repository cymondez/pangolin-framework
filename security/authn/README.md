# JWT 與服務憑證 Adapter

本套件提供 RS256 JWT／JWKS Validator 及既有 ServiceVerifier 的相容 Adapter。通用 Identity、Verifier、完成／Challenge／Pending 結果與私有 context 儲存歸 [security/identity](../identity/README.md)；新增 Session、API Key 或其他登入模組不必修改 JWT 程式。

## JWT 組裝

```go
validator := authn.NewValidator(issuer, audience, jwksURL,
    &http.Client{Timeout: 2*time.Second})
authenticated, err := validator.Authenticate(ctx, bearerToken)
// 只有 err == nil 才將 authenticated 交給 Tenant／Scope Resolver。
```

Validator 實作 identity.Verifier，Kind 為 bearer；Verify 只回驗證結果，Authenticate／AuthenticateCredential 經共同 Pipeline 完成及安裝 identity。authn.Identity 與 authn.ErrUnauthenticated 保留相容轉接；新程式直接使用 identity 契約，不依賴 JWT Adapter 取得身分。NewValidator(issuer, audience, jwksURL, client) 保留既有簽名並使用預設政策；NewValidatorWithPolicy(..., KeyPolicy) 可調整 CacheTTL、FetchCooldown、MaxKeys 與 MaxResponseBytes，非正值一律回退 DefaultKeyPolicy（TTL 5 分鐘、cooldown 5 秒、最多 32 把金鑰、JWKS 上限 64 KiB）。

JWT 驗證 RS256、kid、RSA 簽章、issuer、字串／陣列 audience、exp、nbf，以及非空 sub／tenant_id。RSA modulus 至少 2048 bits，exponent 必須合法。Token 上限 16 KiB，JWKS 上限按政策（預設 64 KiB）。可信 issuer／JWKS URL 來自啟動組態，不能採用請求指定的位置；正式部署使用 HTTPS 與有界 HTTP client。

## 有界 JWKS 金鑰快取

驗證不逐次取 JWKS：快取一次取得的可用金鑰組，於 CacheTTL 內以快取金鑰完成已知 kid 的驗證，因此 TTL 內的金鑰端點故障不影響身分驗證，格式錯誤與 header／claims／kid 等 precheck 失敗也不觸發取用；通過 precheck 的簽章無效 Token 因簽章驗證在取得金鑰之後，仍會進入金鑰路徑再被拒絕。CacheTTL 過期或出現未知 kid 時才刷新：併發刷新合併為唯一在途取用（由發起請求的 goroutine 執行，不啟動背景程序；等待者對自己的 context select，取消即時返回且不造成額外取用），每筆取用另以 FetchTimeout（預設 5 秒）自有 context 逾時包裝，HTTP client 即便 Timeout 0 也有界。每次「開始」的取用 attempt 一律推進 FetchCooldown——節流間距自 attempt 開始起算，與結果或呼叫方取消無關——因此取消洪流不能提高取用頻率，cooldown 過期後獨立呼叫端可重試；已取消的 Verify 不啟動取用、也不消耗預定的 cooldown。cooldown 內未知 kid 與過期重試一律立即 fail closed 不打網路。金鑰組不含 kid map，攻擊 kid 只受 cooldown 節流，無記憶體成長。

Fail closed 政策：只有現值為陣列的 `keys` 成員是權威快照——明確的空陣列替換既有快取（key withdrawal，撤銷的 kid 立即停止驗證）；缺漏、null 或型別錯誤的 `keys`、非 200、超過 MaxResponseBytes 與不可解析的回應視為失敗 attempt，保留未過期既有快取。逾時或取消導致的取用失敗不安裝任何金鑰（取消不改動既有快取）。快取過期且來端不可用時直接拒絕；失敗後 cooldown 內重試不重打來源。Verify 在接受快取身分前、等待刷新後與簽章驗證後都檢查操作取消，已取消的操作一律拒絕且不安裝身分。取用範圍聲明：格式錯誤、header／claims／kid 等 precheck 失敗不觸發取用；通過 precheck 的簽章無效 Token 因簽章驗證必然在取得金鑰之後，仍會進入金鑰路徑（含刷新）再被拒絕。輪替行為：新 kid 會觸發一次受 cooldown 節流的刷新；同 kid 換鑰要到 CacheTTL 過期才會被察覺（標準 JWKS 快取取捨），過期後舊金鑰不再通過驗證。MaxKeys 按文件順序保留可用金鑰，被截斷的 kid fail closed；同 kid 重複金鑰全部保留並逐把嘗試驗證。金鑰快取不取代 Tenant／目前授權檢查；已驗證 Token 的非秘密 amr 可作 assurance metadata，不能代替應用的 step-up 政策。此 Adapter 不負責 OIDC discovery、登入 UI、協定回呼或 Session Store。

## 服務身分與其他登入方式

NewServiceAuthenticator(ServiceVerifier) 保留部署選定 mTLS／OAuth 憑證的入口，經共同 Pipeline 安裝 service identity。底層回傳其他 Principal.Kind、缺 ID／Tenant 或驗證失敗均拒絕。一般使用者 Session／API Key 模組直接實作 identity.Verifier，回傳 user identity，不能借用服務限定入口。

HTTP 使用共用 transport/http 的 CredentialExtractor 對接 Pipeline；預設 Bearer 仍可使用。Cookie 的安全屬性、Session 狀態及寫入時的 CSRF 防護由 Session 模組負責；見 [HTTP 模組](../../transport/http/README.md)。

## 驗證與清理

執行 go test ./security/authn ./security/identity。既有測試使用真實 RSA 簽章及 HTTP JWKS fixture，並以可注入時脈、閘控 transport 與僅測試用 watched context 覆蓋 precheck 排除、過期、金鑰快取、合併刷新（所有 joining者在等待中釋放才判定單次取用）、cooldown、取消領導者節流、FetchTimeout 逾時、key withdrawal、malformed `keys` 保留舊快取、輪替與來源故障。登入後仍須驗證目前 Tenant／Scope／Permission。HTTP client 及外部資源由組裝端擁有；Adapter 不啟動背景程序，也不記錄 Token 或 JWKS 內容。
