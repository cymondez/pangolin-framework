---
title: HTTP 入口與安全稽核（Pipeline & Audit）
description: 用 transport/http.NewPipeline 統一串接驗證、租戶 Scope、權限、解碼與交易，並區分「同交易必要稽核」與「獨立安全拒絕日誌」。
---

> **你在架構中的位置**：`HTTP Request →` **`[ transport/http.NewPipeline（驗證 → Tenant/Scope → Entry Policy → Decode → UOW → Commit → 序列化）]`** `+` **`[ platform/audit ]`**
>
> **前置建議**：先完成[權限與資源政策](authorization.mdx)，並備妥 UOW Manager。

## 為什麼需要共用 HTTP Pipeline？

如果每支 HTTP Handler 都自己手寫「驗 Token → 解 JSON → 查權限 → 開交易 → 寫回應」：
- 很容易發生「**先解碼巨大 JSON body 才發現未授權**」（被未認證流量打爆記憶體），
- 或「**忘了先驗權限就直接開交易**」，
- 或「**還沒確認資料庫 Commit 就先回傳 201，或把內部 SQL 錯誤字串直接暴露給前端**」。

`transport/http.NewPipeline(PipelineOptions)` 把這條標準執行順序固化下來，透過顯式函式相依讓你的每支 API 共享同一套安全護欄。

---

## 1. `PipelineOptions` 的固定執行管線

建立 `PipelineOptions` 時須提供 `Timeout`、`MaxBodyBytes`、`Manager`、`Authenticate`（或 `AuthenticateCredential` + `Credentials`）、`ResolveTenant`、`ResolveScope`、`Entry`、`Decode`、`Work`（缺少任何必要欄位會在建立時直接報錯）。預設執行寫入 `Command`，設定 `ReadOnly: true` 則改執行唯讀 `Query`；另可選填 `ResourcePolicy` 與 `Rejected`。

```text
1. Timeout & Trace  ──▶ 設定整筆請求期限
2. Authenticate     ──▶ 驗證 Bearer JWT／Session Cookie（失敗 → 401 + 記 SecurityLog）
3. ResolveTenant    ──▶ 核對候選租戶與當前資格 Authority（失敗 → 403 + 記 SecurityLog）
4. ResolveScope     ──▶ 安裝不可變 ScopeBinding（失敗 → 403 + 記 SecurityLog）
5. Entry Policy     ──▶ 檢查動作 Permission（失敗 → 403 + 記 SecurityLog，此時尚未解碼 Body！）
6. Decode           ──▶ 在 MaxBodyBytes 上限內解碼並驗證 DTO（失敗 → 400）
7. UOW + Work       ──▶ 開啟交易 → 執行可選 ResourcePolicy → 執行你的 Work Use Case
8. Commit & Respond ──▶ 確認 Commit 成功後才序列化並寫回 2xx；失敗統一回 Problem Details
```

### 精簡多路由組裝：`NewEndpoints`、`ProblemError` 與 `ServerParticipant`

當同一個服務有多條 HTTP 路由時，不需要在每條路由重複組裝 `PipelineOptions` 或對 `any` 做型別斷言。建議在啟動期建立一次 `transport/http.NewEndpoints(EndpointConfig)`，再用泛型建構函式直接產生 `http.Handler`：

```go
endpoints, err := transport.NewEndpoints(transport.EndpointConfig{
	Timeout:                5 * time.Second,
	MaxBodyBytes:           1 << 20,
	Manager:                manager,
	AuthenticateCredential: validator.AuthenticateCredential,
	Credentials:            transport.BearerCredentials(),
	ResolveTenant:          resolveTenant,
	ResolveScope:           resolveScope,
	Authorize:              scopes.Authorize,
	Rejected:               onRejected,
})

createBook, err := transport.CommandEndpoint(endpoints, func(ctx context.Context, in transport.Input[domain.Book]) (domain.Book, error) {
	created, createErr := books.Create(ctx, in.Body, in.IdempotencyKey)
	if database.IsConflict(createErr) {
		return domain.Book{}, transport.NewProblemError(http.StatusConflict, "CONFLICT", createErr)
	}
	return created, createErr
}, transport.WithPermission("books.write"), transport.WithIdempotencyHeader("Idempotency-Key", true))

getBook, err := transport.QueryEndpoint(endpoints, func(ctx context.Context, in transport.QueryInput) (domain.Book, error) {
	book, getErr := books.Get(ctx, in.Request.PathValue("id"))
	if database.IsNotFound(getErr) {
		return domain.Book{}, transport.NewProblemError(http.StatusNotFound, "NOT_FOUND", getErr)
	}
	return book, getErr
}, transport.WithPermission("books.read"))
```

- **嚴格 JSON 解碼與自動驗證**：`CommandEndpoint` 內建 `DisallowUnknownFields`、拒絕 trailing tokens，並自動呼叫請求型別可選的 `Validate() error`（失敗回 `422 VALIDATION_FAILED`）；`WithStatus` 可自訂 `2xx` 狀態碼（如 `201 Created`），`WithResourcePolicy[Req]`／`WithResourceCheck` 於交易內執行資源檢查。
- **標準錯誤分類**：使用 `transport.NewProblemError(status, code, cause)` 直接對接 `WriteError`，免除應用層重複定義錯誤 struct，同時保留原始 `cause` 不洩漏給外部用戶端。
- **內建停機協調**：使用 `transport.NewServerParticipant(runtime.NewWorkGroup())` 搭配 `Attach(server)`、`Add(extra...)` 與 `Wrap(mux)`，直接交給 `runtime.New(...)` 完成優雅停機與 Drain。

### 重要使用邊界
- **單次非串流請求專用**：本 Pipeline 適用於一般 REST／JSON API。若是 **SSE 或 WebSocket 長連線**，請改用[工作單元](../data/unit-of-work.md)提到的 `StreamOperation` 為每次互動開啟短交易，切勿用一筆長交易包住整條長連線。
- **路由／Header 候選防衝突**：使用 `scope.Select` 拒絕路由與 Header 互相矛盾的請求；若資源 DTO 帶有 owner 欄位，務必使用 `scope.MatchOwner` 與可信 binding 比對，絕不拿 DTO 覆蓋 owner。
- **敏感 Use Case 雙重防線**：即使入口 `Entry` 已經驗過一次權限，高敏感的 Application Use Case 內部仍應搭配 `policy.Require` 或 `application.AuthorizedCommand`，防範日後由背景 Job 或其他入口呼叫時漏驗。

---

## 2. 為什麼稽核記錄分成「兩種」？

很多系統把「操作稽核」與「登入／權限被拒日誌」寫在同一個地方，結果一發生 `Rollback` 就把攻擊紀錄一起回滾不見了！panGOlin 明確區分兩條管道：

| 記錄類型 | 使用 API | 寫入時機與交易關係 | 適用場景 |
|---|---|---|---|
| **必要業務稽核（Audit）** | `platform/audit.Recorder.Record` | **與業務寫入／Outbox 寫在同一筆可寫 UOW 交易內**（須先安裝 `audit.Migrations`）。若寫稽核失敗**必須回傳錯誤讓整筆交易回滾**，確保沒有「做了敏感操作卻沒留稽核」的破口。 | 建立／修改關鍵資源、角色授權變更、人工重送死信事件 |
| **安全拒絕日誌（Security Log）** | `audit.NewSecurityLog(writer, authn.Identity)`（接到 Pipeline 的 `Rejected`） | **完全獨立於業務交易**，即使業務未開啟交易或交易已回滾，拒絕事件仍會安全留存。 | 登入失敗（401）、跨租戶嘗試（403）、Permission 拒絕（403） |

### 稽核內容最小化與遮罩原則

- `audit.Record` 只接受受控的結構化欄位：`Action`、`Resource`、`Result`、`Trace` 與非負 `Duration`（識別欄位限 1–160 字元，僅允許英數與 `_ . : -`）。
- **Actor 與完整 Owner 一律從可信 context 自動填入**，絕不接受隨意的 Request Body、原始 Token、SQL 語句或未遮罩的內部錯誤訊息。
- 當尚未綁定 Scope 就被拒絕時，Security Log 只會記錄已驗證的 Actor，不會把攻擊者亂填的未授權 owner hint 當成真實歸屬記下來。

---

## 3. 提交後失敗與 Host 跨租戶操作

- **Problem Details 錯誤遮罩**：所有回傳給外部用戶端的錯誤皆會遮罩內部堆疊與 SQL 細節。
- **提交後序列化或斷線**：如果資料庫已經確認 Commit，但寫回 HTTP 回應時失敗，回應 metadata 會保留操作識別與 `Committed` outcome；若遇到 `UOW_COMMIT_UNKNOWN`，請用戶端保留原 `Idempotency-Key` 依[結果對帳](../data/idempotency.md)查詢，切勿換新 key 重送。
- **Host 系統級跨租戶控制面**：極少數平台管理員需要跨租戶查詢時，**絕不可**為一般業務 Repository 開放「空 Tenant」或「萬用 Scope」；必須走獨立受稽核的 `policy.Host.Run` 控制面，明示目標 Tenant、action、Host Policy 與必要 Audit。

## 下一步

安全與入口防線全部就位！接下來進入事件驅動架構：先從 **[使用 Integration Event](../events/integration-events.mdx)** 開始，再學會 **[組裝可靠事件配送](../events/reliable-delivery.md)**。
