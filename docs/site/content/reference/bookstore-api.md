---
title: Bookstore HTTP API 參考
description: 框架內建 Bookstore 範例 API 的端點規格、JSON 格式、Tenant Header 規則與 RFC 9457 Problem Details 錯誤碼處理指引。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

> **適用範圍提醒**：本頁記載的是框架原始碼內建的 **`examples/bookstore`** API（建立回 **201**、含 `version` 欄位）。若使用 CLI 生成的獨立專案（建立與讀取皆回 **200**、僅含 `id`/`title`），請查閱 **[pangolin CLI 參考](cli.md)** 與 **[用 CLI 建立自己的 Bookstore](../tutorials/generated-bookstore.md)**。

除 `/readyz` 健康檢查外，所有端點皆須提供有效認證標頭：`Authorization: Bearer <access_token>`（操作流程見 [Bookstore 教學](../tutorials/bookstore.md)）。

---

## 1. 端點一覽

| 方法與路徑 | 成功狀態碼 | 說明 |
|---|---|---|
| `GET /readyz` | `204 No Content` | 檢查 API 與資料庫是否就緒；尚未 Ready 或相依異常回 `503` |
| `POST /books` | `201 Created` | 建立書籍；**必須等資料庫確認 Commit（含 Outbox）後才回傳** |
| `GET /books/{id}` | `200 OK` | 查詢目前受信任 Tenant 內的單一書籍 |

---

## 2. 請求與回應格式

### `POST /books` 請求體
- 上限 **1 MiB**，嚴格拒絕未知欄位與多份串接 JSON。
- `id` 可由客戶端預先產生（方便對帳）；`title` 不可空白且最長 **200 bytes**。

```json
{"id": "client-generated-id", "title": "My first panGOlin book"}
```

### `POST /books`（201）與 `GET /books/{id}`（200）成功回應

```json
{"id": "client-generated-id", "title": "My first panGOlin book", "version": 1}
```

### 快速呼叫範例

取得本機示範 Token 後（詳見 [Bookstore 教學](../tutorials/bookstore.md)），可直接透過終端機呼叫：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 檢查 API 健康狀態（預期 HTTP 204）
(Invoke-WebRequest http://127.0.0.1:18181/readyz).StatusCode

# 建立與讀取書籍（請先設定 $headers = @{ Authorization = "Bearer <access_token>" }）
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:18181/books `
  -Headers $headers -ContentType application/json `
  -Body '{"id":"book-1","title":"My first panGOlin book"}'
Invoke-RestMethod -Uri http://127.0.0.1:18181/books/book-1 -Headers $headers
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 檢查 API 健康狀態（預期 HTTP 204）
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18181/readyz

# 建立與讀取書籍（請先設定 TOKEN='<access_token>'）
curl -fsS -X POST http://127.0.0.1:18181/books \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"id":"book-1","title":"My first panGOlin book"}'
echo
curl -fsS http://127.0.0.1:18181/books/book-1 \
  -H "Authorization: Bearer $TOKEN"
echo
```

</TabItem>
</Tabs>

---

## 3. Tenant 與 Scope 候選標頭規則

| Header | 規則與安全行為 |
|---|---|
| `X-Tenant-ID` | 可選的候選 Tenant；省略時使用 JWT 的 `tenant_id`。若與已驗證身分不符，直接回 **`403`**。 |
| `X-Scope-Kind` | 預設 Bookstore 僅接受 `tenant`；傳入其他 Kind 回 **`400 SCOPE_KIND_UNSUPPORTED`**（不會偷偷當成 tenant）。 |
| `X-Scope-ID` | 必須等於受信任的 Tenant ID；否則拒絕。 |

---

## 4. 錯誤回應格式（Problem Details）與客戶端處理指引

所有錯誤回應皆採用 `Content-Type: application/problem+json`，自動遮罩內部 SQL 與堆疊細節：

```json
{
  "type": "about:blank",
  "title": "Unauthorized",
  "status": 401,
  "detail": "Unauthorized",
  "instance": "/books",
  "traceId": "",
  "code": "UNAUTHENTICATED"
}
```

| HTTP 狀態 | 錯誤碼（`code`） | 原因與建議處置方式 |
|---|---|---|
| `400` | `MALFORMED_REQUEST` | JSON 語法錯誤、超過 1 MiB 上限或夾帶未知欄位 → 修正請求體 |
| `400` | `SCOPE_KIND_UNSUPPORTED` | 傳入了預設 Bookstore 未啟用的 Scope Kind → 改用 `tenant` 或改搭組織 Profile |
| `401` | `UNAUTHENTICATED` | 缺少 Token、驗簽失敗或 5 分鐘效期已過 → 重新向 IdP 登入換發 Token |
| `403` | `PERMISSION_DENIED` | 嘗試跨租戶存取或缺少必要權限 → 核對 `X-Tenant-ID` 與目前登入身分 |
| `404` | `NOT_FOUND` | 該書籍不存在（或屬於其他租戶）→ 確認 `id` 與目前 Tenant |
| `409` | `CONFLICT` | 重複建立相同 `id` 或版本衝突 → 確認既有資料或改用新 `id` |
| `422` | `VALIDATION_FAILED` | 書名為空或超過 200 bytes → 修正業務欄位 |
| `503` | `DEPENDENCY_UNAVAILABLE` | 應用尚未就緒或相依資料庫暫時不可達 → 退避後重試 |
| `503` | `UOW_COMMIT_UNKNOWN` | **提交結果未知**（回應附 `outcome: "unknown"` 與 `operationId`）→ **切勿直接換新 ID 重送！** 請用發送前保存的書籍 `id` 查詢對帳（注意：查一次沒有結果仍不能單憑它斷定原操作未提交；`operationId` 本身也不授予查詢權限） |

未分類的內部錯誤統一回傳 `500`／`INTERNAL`。
