---
title: 可擴充登入與身分驗證
description: 用共同 Identity 契約接上 JWT、Session 或自訂登入模組，再驗證 Tenant、Scope 與 Permission。
---

登入模組驗證憑證，security/identity 的 Pipeline 統一安裝可信身分，HTTP 入口再解析當前 Tenant／Scope 並檢查 Permission。JWT 是一個 Adapter；Session、API Key 或未來 MFA、OIDC、Device Login 模組都使用相同擴充 Port，不需修改核心。

## 選擇驗證方式

| 部分 | 模組負責 | 共用契約 |
|---|---|---|
| HTTP 憑證 | 從 Header、Cookie 或其他明示來源擷取 | transport/http.CredentialExtractor |
| 驗證流程 | 查驗簽章、Session 狀態或登入流程 | identity.Verifier |
| 完成／待完成 | 回傳 Principal、Tenant hint、方式或 Challenge | identity.Result |
| 可信 context | 檢查結果、取消與 private key 安裝 | identity.Pipeline |
| 存取資格 | 查目前 Tenant、Scope、Permission | Tenant／Scope Resolver 與政策 |

請求 body 的使用者 ID、Tenant 或 Scope 都不是授權來源。組裝端只註冊可信 Verifier，不能將 Principal DTO 當成已驗證身分。

## 接上自己的 Session 模組

以下完整檔案可放在應用的 authentication/session.go。SessionStore 由你的登入模組實作，需查驗 Session 的有效期限、撤銷狀態與目前使用者；此頁不以記憶體 Token 表充當正式 Session Store。

```go
package authentication

import (
    "context"
    "github.com/cymondez/pangolin-framework/security/identity"
    "github.com/cymondez/pangolin-framework/security/scope"
    transport "github.com/cymondez/pangolin-framework/transport/http"
)

type SessionStore interface {
    Resolve(context.Context, string) (scope.PrincipalRef, string, error)
}

type Sessions struct { Store SessionStore }

func (Sessions) Kind() string { return identity.SessionKind }

func (s Sessions) Verify(ctx context.Context, credential identity.Credential) (identity.Result, error) {
    if s.Store == nil || credential.Kind != identity.SessionKind {
        return identity.Result{}, identity.ErrUnauthenticated
    }
    principal, tenant, err := s.Store.Resolve(ctx, credential.Value)
    if err != nil { return identity.Result{}, err }
    if principal.Kind != identity.PrincipalUser {
        return identity.Result{}, identity.ErrUnauthenticated
    }
    return identity.Result{
        Status: identity.StatusAuthenticated, Principal: principal,
        Tenant: tenant, Method: "session",
    }, nil
}

func NewSessionAuthentication(store SessionStore) (*identity.Pipeline, transport.CredentialExtractor, error) {
    if store == nil { return nil, nil, identity.ErrUnauthenticated }
    pipeline, err := identity.NewPipeline(Sessions{Store: store})
    if err != nil { return nil, nil, err }
    return pipeline, transport.CookieCredentials("pangolin_session"), nil
}
```

在你的 HTTP 組裝端，將 pipeline.AuthenticateCredential 傳入 PipelineOptions.AuthenticateCredential，將 extractor 傳入 Credentials；其餘 Manager、ResolveTenant、ResolveScope、Entry 與 Work 依原本的 UOW／政策設定。Bookstore Handler 對應欄位為 CredentialAuth 與 Credentials。舊 Authenticate／Auth 保留 Bearer 相容入口，兩種模式擇一設定。

CookieCredentials 只接受 GET／HEAD／OPTIONS，存在 Cookie 的其他方法回 COOKIE_UNSAFE_METHOD。需要 Cookie 寫入時，由 Session 模組提供完成 CSRF 查驗的 extractor，並自行設定 HttpOnly、Secure、SameSite、到期與撤銷策略；不能把方法檢查直接移除就當作安全接線。

## 同時提供 JWT 與其他憑證

RS256 Adapter 仍以 authn.NewValidator(issuer, audience, jwksURL, client) 建立。它實作 identity.Verifier，可與 Session Verifier 一起交給 NewPipeline；HTTP 使用 OneCredential(BearerCredentials(), CookieCredentials(...))。同一請求同時提供多份憑證會拒絕，格式錯誤或驗證失敗也不改用另一種身分。

issuer、audience 與 JWKS URL 來自可信啟動組態，不從請求指定位置下載金鑰；正式部署用 HTTPS 與有界 HTTP client。JWT 會檢查 RSA 簽章、issuer、audience、有效期限、sub 與 tenant_id；見 [Bookstore](../tutorials/bookstore.md)的本機 Token 操作。

## MFA、OIDC 與 Device Login

協定模組自行擁有登入／redirect／polling endpoint、驗證狀態、一次性 Token 與重播防護。需要後續驗證回 StatusChallenge，等待核准回 StatusPending；共同 Pipeline 分別回 ChallengeError／PendingError，均不安裝可授權身分。整個協定完成後才回 StatusAuthenticated。這些擴充契約已提供，完整協定模組仍需另行實作。

驗證成功後讀取 identity.Identity(authenticated)，登入方式與 assurance 可由 identity.Method 取得；context 不儲存原始憑證。失敗或取消即結束請求，不沿用驗證前的 context。

下一步[建立當前 Tenant 資格](multi-tenancy.md)，再解析 Scope 與 Permission。登入成功不代表能存取任何租戶或資源；服務入口也維持獨立 service Principal，不能以使用者身分替代。
