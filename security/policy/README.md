# Permission 與 Resource Policy

`Allowed(Rules)` 對單一 Scope 的權威快照套用 User deny 優先，之後依 User、Role、Tenant、System 尋找允許，預設拒絕。Feature／Settings 不參與授權，Role 不跨 owner 傳播。

敏感 Application Use Case 呼叫 `Require(ctx, authorizer, permission, resourcePolicy)`；authorizer 由 constructor 注入，通常為 Scope Registry。必須具有可信 binding，所有必要檢查皆通過才放行。Resource Policy 在 UOW 中載入資源後執行；錯誤保留 cause，由 Manager 回滾。套件沒有持有資源，無關閉責任。

驗證：倉庫根目錄執行 `go test ./security/policy`。權威快照與上層治理限制由所選 Provider 負責，不能把客戶端 DTO 當作 Rules。

`Host.Run` 是獨立控制面 Port：明示 target Tenant／action，先驗 Host Policy 與必要 Audit，才呼叫受限制 Query Port。不安裝空 Tenant／萬用 binding，不授予一般業務連線 bypass；Audit 失敗就拒絕執行。
