---
title: 驗證登入身份
description: 使用可信 issuer 與 JWKS 驗證 Bearer Token，再交給應用處理。
---

當你的 API 需要知道「誰正在呼叫」，可以使用 `security/authn` 驗證 JWT（JSON Web Token，攜帶可驗證登入資訊的 Token）。JWKS 是登入服務提供的公開驗證金鑰集合。先在 [Bookstore](../tutorials/bookstore.md)取得可用 Token，再閱讀這一頁。

## 你需要準備的資料

| 值 | 從哪裡取得 |
|---|---|
| issuer | 你的登入服務所宣告的可信發行者 |
| audience | 發給目前應用的 Token 受眾 |
| JWKS URL | 登入服務公開 RSA 驗證金鑰的位置 |
| bearerToken | 本次 HTTP 請求的 Authorization Token |

在 Bookstore，audience 是 `pangolin-bookstore`，本機 issuer 是 `http://127.0.0.1:18180`，JWKS 位於該 origin 的 `/jwks.json`。正式登入服務應使用 HTTPS。

## 在 HTTP 入口驗證

```go
validator := authn.NewValidator(issuer, audience, jwksURL,
    &http.Client{Timeout: 2 * time.Second})
authenticated, err := validator.Authenticate(request.Context(), bearerToken)
if err != nil {
    // 回應 401，結束這次請求。
    return
}
```

此為 HTTP handler 內的呼叫片段，使用 `security/authn`、`net/http` 與 `time`。issuer／audience／JWKS URL 由你的可信啟動設定傳入，不從請求指定的位置下載金鑰。

成功後，將 authenticated context 交給後續處理；不要繼續使用驗證前的 request context。`authn.Identity(authenticated)` 可以讀出 principal 與 Tenant。

## 驗證成功後

接著[建立 Tenant Scope](multi-tenancy.md)，再呼叫資料操作。登入成功表示身份可信，不代表能存取任意租戶或資源。

服務入口可透過 `authn.NewServiceAuthenticator(verifier)` 注入部署選定的服務憑證驗證器，取得獨立 service Principal；不能把使用者 ID 改名成服務身分。Event／Job 每次以目前 grant 重建 context，見[可靠配送](../events/reliable-delivery.md)。

目前支援 RS256／JWKS，檢查簽章、issuer、audience、有效期限、sub 與 tenant_id。過期、來源無法取得或非法 Token 都拒絕；不要把 Token 放進記錄或錯誤回應。
