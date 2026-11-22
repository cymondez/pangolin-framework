---
title: Bookstore HTTP API
description: Bookstore 的登入要求、書籍操作與錯誤回應。
---

先依 [Bookstore 教學](../tutorials/bookstore.md)啟動 API 並取得 Token。除 readiness 外，請求都需要 `Authorization: Bearer ...`。

| 方法與路徑 | 成功狀態 | 用途 |
|---|---|---|
| `GET /readyz` | 204 | 檢查 API 是否能接受請求，未準備完成時為 503 |
| `POST /books` | 201 | 建立書籍，資料確認提交後回應 |
| `GET /books/{id}` | 200 | 查詢目前 Tenant 的一本書 |

## 請求與回應

POST body：

```json
{"id": "client-generated-id", "title": "My first panGOlin book"}
```

成功回應：

```json
{"id": "client-generated-id", "title": "My first panGOlin book", "version": 1}
```

GET 回應包含相同欄位。id 可預先產生；title 必須為非空、最長 200 bytes 的書名。POST body 上限 1 MiB，不接受未知欄位或多份 JSON。

## Tenant Headers

`X-Tenant-ID` 可提出候選 Tenant，預設使用 JWT 的 tenant。`X-Scope-Kind` 目前只接受 tenant，`X-Scope-ID` 必須等於該 Tenant。跨租戶或不允許的範圍會被拒絕。

## 錯誤回應

回應 Content-Type 為 `application/problem+json`：

```json
{"type":"about:blank","title":"Unauthorized","status":401,
 "detail":"Unauthorized","instance":"/books","traceId":"","code":"UNAUTHENTICATED"}
```

| 狀態 | code | 你的處理方式 |
|---|---|---|
| 400 | MALFORMED_REQUEST | 修正 JSON／未知欄位 |
| 400 | SCOPE_KIND_UNSUPPORTED | 預設 Bookstore 只接受 tenant Kind |
| 401 | UNAUTHENTICATED | 檢查 Token 或重新登入 |
| 403 | PERMISSION_DENIED | 檢查 Tenant／Scope，避免跨租戶存取 |
| 404 | NOT_FOUND | 確認路徑、id 與目前 Tenant |
| 409 | CONFLICT | 改用新 id 或確認既有資料 |
| 422 | VALIDATION_FAILED | 修正書籍欄位 |
| 503 | DEPENDENCY_UNAVAILABLE | 等應用準備完成或確認相依服務 |
| 503 | UOW_COMMIT_UNKNOWN | 用原操作 id 查詢／對帳，避免直接重送 |

其他未分類錯誤回應 500／INTERNAL。查詢一次沒有結果，不能單憑它判斷未知的提交結果已確認回滾。

Unknown 回覆包含 `outcome: "unknown"` 與 operationId，不提供無條件 retryable。提交後回應遺失時，使用發送前已保存的書籍 id 對帳；operationId 本身不會授予讀取結果的權限。
