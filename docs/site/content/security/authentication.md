---
title: 可擴充登入與身分驗證（Authentication）
description: 理解 panGOlin 如何將「憑證擷取」「登入驗證」「可信身分安裝」拆開，無痛支援 JWT、Session 或自訂驗證模組。
---

> **你在架構中的位置**：`HTTP 入口 → Trace/Timeout →` **`[ Authenticate（擷取憑證 → Verifier 驗證 → Pipeline 安裝可信身分）]`** `→ Resolve Tenant → Resolve Scope → Permission`
>
> **前置建議**：先閱讀[架構總覽](../concepts/architecture.md)理解請求管線全貌。

## 為什麼 panGOlin 不把身分驗證寫死成 JWT？

很多框架假設「所有 API 都只用 Bearer JWT」，結果當團隊後來需要支援：
- 瀏覽器後台使用的 **Cookie Session**，
- 外部系統串接用的 **API Key**，
- 或未來導入 **MFA、OIDC、Device Login**，

就得把整個 HTTP Middleware 跟下游 Service 全部大改一次。

panGOlin 把身分驗證拆成 **「外掛式 Verifier + 統一可信 Pipeline」**：

```text
HTTP 請求
  │
  ▼  1. transport/http.CredentialExtractor
  │     從明確來源擷取憑證（如 Authorization: Bearer ... 或 Cookie）
  │     （注意：同一請求同時帶兩份憑證一律拒絕，失敗也不降級改試另一種！）
  ▼  2. identity.Verifier（可插拔：JWT Adapter / Session / API Key / 自訂）
  │     查驗簽章、有效期或 Session 狀態，回傳 identity.Result
  ▼  3. identity.Pipeline
  │     驗證結果狀態 → 確認未取消 → 以「私有 context key」安裝不可偽造的可信身分
  ▼  4. 下游 Tenant / Scope Resolver 與 Permission
        只認 identity.Identity(ctx)，完全不需要知道使用者剛才是用 JWT 還是 Session 登入！
```

> **核心安全原則**：**登入驗證只回答「你是誰」**。驗證成功**不代表**你能存取任何租戶或業務資料——接著仍必須通過[多租戶資格檢查](multi-tenancy.md)與[權限檢查](authorization.mdx)。

---

## 1. 五個核心契約的分工

| 環節 | 負責什麼 | 框架公開契約 |
|---|---|---|
| **HTTP 憑證擷取** | 從 Header、Cookie 或其他明示位置取出憑證（絕不讀 Request Body） | `transport/http.CredentialExtractor` |
| **憑證驗證邏輯** | 查驗 RSA 簽章、查 Session Store 或執行登入協定 | `identity.Verifier` |
| **驗證結果狀態** | 表達已驗證、需進一步驗證（Challenge）或等待核准（Pending） | `identity.Result` |
| **可信身分安裝** | 統一檢查結果與取消狀態，用私有 key 寫入 context（不存原始憑證） | `identity.Pipeline` |
| **後續存取資格** | 確認目前租戶資格、綁定 Scope、檢查 Permission | `tenancy.Resolver` / `scope.Registry` / `policy` |

---

## 2. 實戰範例：接上你自己的 Session 模組（`authentication/session.go`）

以下完整檔案示範如何實作一個可插入 `identity.Pipeline` 的 Session Verifier。`SessionStore` 由你自己的登入模組實作，須實際查驗 Session 有效期、撤銷狀態與對應使用者（切勿用簡易記憶體表充當正式 Session Store）：

```go
package authentication

import (
	"context"

	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/scope"
	transport "github.com/cymondez/pangolin-framework/transport/http"
)

// SessionStore 是你自己的登入模組介面，負責向持久化儲存查驗 Session 是否仍有效且未撤銷。
type SessionStore interface {
	Resolve(ctx context.Context, sessionID string) (principal scope.PrincipalRef, tenant string, err error)
}

// Sessions 實作框架的 identity.Verifier 介面，讓 Session 登入無縫融入共用 Pipeline。
type Sessions struct {
	Store SessionStore
}

// Kind 宣告這個 Verifier 專門處理 Session 憑證類型。
func (Sessions) Kind() string { return identity.SessionKind }

// Verify 驗證傳入的憑證，並回傳標準 identity.Result。
func (s Sessions) Verify(ctx context.Context, credential identity.Credential) (identity.Result, error) {
	// 步驟 1：防禦性檢查 Store 與憑證型別。
	if s.Store == nil || credential.Kind != identity.SessionKind {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	// 步驟 2：向權威 SessionStore 查詢目前有效的使用者與租戶提示（Tenant hint）。
	principal, tenant, err := s.Store.Resolve(ctx, credential.Value)
	if err != nil {
		return identity.Result{}, err
	}
	// 步驟 3：僅允許明確的使用者身分（PrincipalUser），防止誤把服務帳號當成使用者 Session。
	if principal.Kind != identity.PrincipalUser {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{
		Status:    identity.StatusAuthenticated,
		Principal: principal,
		Tenant:    tenant,
		Method:    "session",
	}, nil
}

// NewSessionAuthentication 組裝共用 identity.Pipeline 與 Cookie 憑證擷取器，供 HTTP 入口使用。
func NewSessionAuthentication(store SessionStore) (*identity.Pipeline, transport.CredentialExtractor, error) {
	if store == nil {
		return nil, nil, identity.ErrUnauthenticated
	}
	pipeline, err := identity.NewPipeline(Sessions{Store: store})
	if err != nil {
		return nil, nil, err
	}
	return pipeline, transport.CookieCredentials("pangolin_session"), nil
}
```

### 怎麼接到 HTTP 管線？

在你的 HTTP 組裝端：
- 將 `pipeline.AuthenticateCredential` 傳給 `transport.PipelineOptions.AuthenticateCredential`。
- 將回傳的 `extractor` 傳給 `PipelineOptions.Credentials`（在 Bookstore Handler 中對應欄位為 `CredentialAuth` 與 `Credentials`）。
- 舊版僅支援 Bearer Token 的 `Authenticate`／`Auth` 欄位仍保留相容，兩組模式**擇一設定**即可。

> **Cookie CSRF 安全保護**：內建的 `transport.CookieCredentials(...)` **只允許安全方法（`GET`／`HEAD`／`OPTIONS`）**；若偵測到帶 Cookie 的 `POST`/`PUT`/`DELETE` 請求會直接拒絕並回 `COOKIE_UNSAFE_METHOD`。如果你需要支援 Cookie 寫入，必須由 Session 模組提供已完成 CSRF 驗證的自訂 `CredentialExtractor`，並正確設定 Cookie 的 `HttpOnly`、`Secure`、`SameSite`、到期與撤銷策略——**切勿直接繞過方法檢查**。

---

## 3. 同時支援 JWT（RS256）與 Session

內建的 RS256 JWT Adapter 本身也實作了 `identity.Verifier`：

```go title="同時掛載 JWT 與 Session Verifier ✅"
jwtVerifier, err := authn.NewValidator(issuer, audience, jwksURL, httpClient)
// 把多個 Verifier 交給同一個 identity.Pipeline
pipeline, err := identity.NewPipeline(jwtVerifier, Sessions{Store: sessionStore})
// HTTP 入口使用 OneCredential：單一請求只能帶一種憑證
extractor := transport.OneCredential(
	transport.BearerCredentials(),
	transport.CookieCredentials("pangolin_session"),
)
```

### JWT 與多憑證的安全鐵律

1. **禁止多憑證混送與失敗降級**：`OneCredential` 發現同一請求同時送了 Bearer Header 與 Session Cookie 時直接拒絕；若其中一種憑證格式錯誤或驗簽失敗，**絕不會**自動改用另一種憑證當作備案。
2. **信任組態來源**：`issuer`、`audience` 與 `jwksURL` 必須來自啟動組態，**絕不從請求標頭或 Token 裡的未驗證欄位動態下載金鑰**；正式部署務必使用 HTTPS 與有逾時上限的 `http.Client`。
3. **驗證內容**：RS256 Adapter 會完整檢查 RSA 簽章、`iss`、`aud`、有效期限、`sub` 與 `tenant_id`（可於 [Bookstore 教學](../tutorials/bookstore.md) 實測）。

---

## 4. 擴充 MFA、OIDC 與 Device Login 的狀態邊界

目前 0.1.0 已提供多步驟登入協定的狀態契約（完整協定模組需由應用或獨立模組實作）：
- 需要二次驗證（如 OTP）時回傳 `identity.StatusChallenge` → Pipeline 回傳 `ChallengeError`。
- 等待外部核准（如 Device Login 輪詢）時回傳 `identity.StatusPending` → Pipeline 回傳 `PendingError`。
- **兩者皆不會安裝可授權身分**；必須等到整個協定完成並回傳 `StatusAuthenticated` 才會安裝可信身分。

驗證成功後，下游可用 `identity.Identity(ctx)` 取得身分、用 `identity.Method(ctx)` 取得登入方式與 assurance；**原始 Token／Session 字串絕不會被存進 context**。

## 下一步

確認了「你是誰」之後，立刻前往 **[多租戶與資料隔離](multi-tenancy.md)**，學習如何核對當前租戶資格並安裝不可偽造的 `ScopeRef`。
