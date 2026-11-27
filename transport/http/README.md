# transport/http

共用入口機制，不認識 Bookstore 模型。先在外層驗證身分、Tenant、Scope 與入口 Policy，再使用 `Command(manager, callback)`。它擁有一個頂層可寫交易，僅在 Within 確認提交後序列化並寫 2xx；傳入已有 UOW 的 context 會拒絕，避免 Required 子範圍提早回覆。

`WriteError` 輸出安全 Problem Details。Unknown 優先於 timeout，503 附 `outcome=unknown` 與 operationId，不提供無條件重試保證。提交後序列化失敗為 500／committed；傳輸失敗保留 operationId 日誌，不改寫 DB 結果。

串流連線使用沒有 UOW 的 context；每次資料庫工作各呼叫 `StreamOperation`，操作結束立即釋放交易，不能返回 cursor。寫入串流操作可在自己的 Application Command 內建立短範圍，另以協定確認結果。完整授權、重播及冪等仍由應用政策處理。

`NewPipeline(PipelineOptions)` 將 Authenticate → ResolveTenant → ResolveScope → Entry → body 大小／Decode → Within → ResourcePolicy → Work 固定為可驗證順序。Timeout、MaxBodyBytes 及所有必要 Port 啟動時必填；每階段保留已驗證的 context。拒絕與業務回滾後的 Rejected callback 走獨立安全日誌，callback 故障不改寫交易結果。查詢使用短唯讀 UOW，寫入使用 Command；body DTO 的 owner 必須另以 `scope.MatchOwner` 驗證，不能由 body 決定授權。

提交後自訂 MarshalJSON 返回錯誤或 panic 都保留 committed／operationId，Outcome Unknown 優先於任何一般錯誤分類。可識別安全錯誤與支援 `HTTPStatus()/Code()` 的已分類錯誤映射為安全 Problem Details，不直接輸出 cause。

資源與 CleanupTimeout／HookTimeout 由 UOW Manager 管理，本 package 不關閉 DB。驗證：`go test ./transport/http`、`go test -tags=integration ./tests/m2 ./tests/m3`。

## 可擴充身分驗證

PipelineOptions.AuthenticateCredential 接受完整 identity.Credential，通常接 identity.Pipeline.AuthenticateCredential；Credentials 負責 HTTP 擷取，未指定時使用 BearerCredentials。既有 Authenticate 保留 token-string Bearer 入口，兩種驗證模式必須擇一；舊模式拒絕其他 Credential.Kind。

OneCredential 組合多個 extractor，每次請求最多接受一份憑證。非空但格式錯誤、重複 Authorization、重複指定 Cookie、同時提供多個來源或驗證失敗都拒絕，不猜測另一種身分。Bearer scheme 大小寫不敏感。

CookieCredentials 只接受存在的命名 Session Cookie，限 GET／HEAD／OPTIONS；unsafe method 回 COOKIE_UNSAFE_METHOD。Session 模組可提供完成 CSRF 驗證的 extractor，並自行管理 Cookie 安全屬性與 Session 狀態。沒有 Cookie 的 Bearer POST 不受 Cookie extractor 影響。

Challenge／Pending 分別映射 AUTHENTICATION_CHALLENGE_REQUIRED／AUTHENTICATION_PENDING，不繼續 ResolveTenant／Scope／Use Case。擴充契約與生命週期見 [Identity](../../security/identity/README.md)；完整 MFA／OIDC／Device Login 流程由各模組提供。

## 泛型 Endpoint Factory、ProblemError 與 ServerParticipant

- `NewEndpoints(EndpointConfig)` 在啟動期綁定一次共用元件（`Timeout`、`MaxBodyBytes`、`Manager`、`Authenticate`／`AuthenticateCredential`、`Credentials`、`ResolveTenant`、`ResolveScope`、`Authorize`、`Rejected`），搭配 `CommandEndpoint[Req, Res]` 與 `QueryEndpoint[Res]` 直接產出 `http.Handler`，免去每條路由於請求期重建 `Pipeline` 與手動 `any` 轉型。沿用與 `Pipeline` 相同的嚴格順序與安全護欄，內建嚴格 JSON 解碼（`DisallowUnknownFields`、拒絕 trailing tokens、可選 `Validate() error` 自動回 `422 VALIDATION_FAILED`），並支援 `WithPermission`、`WithEntry`、`WithStatus`、`WithIdempotencyHeader`、`WithResourcePolicy[Req]`、`WithResourceCheck`、`WithTimeout`、`WithMaxBodyBytes`；未設定任何入口檢查或缺少 `Authorize` 時啟動拒絕（fail-closed）。
- `NewProblemError(status, code, cause)` 提供標準 `*ProblemError`（實作 `HTTPStatus() int`、`Code() string`、`Unwrap() error`），直接對接 `WriteError`，不洩漏內部 cause。
- `NewServerParticipant(work, extra...)` 提供內建 `runtime.Participant` 實作，支援 `Attach(*http.Server)`、`Add(extra...)`、`Wrap(http.Handler)`，統一處理關閉 Keep-Alive、停止 WorkGroup 接入與有界 Drain／Cancel。
