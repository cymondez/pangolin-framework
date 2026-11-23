# application

`Command(ctx, manager, callback)` 明示 ReadWrite／Transactional；`Query` 明示 ReadOnly。HTTP、Job、Consumer 呼叫同一 Application Use Case，入口方法或反射不會改寫交易政策。直接使用 Manager 的零值選項仍按規範預設 ReadOnly。

callback 僅使用傳入的 context。返回 nil 是要求完成，仍可能因 Commit／取消／rollback-only 失敗；呼叫方保留 cause 與 Outcome，不自行提交或重跑。資源與清理由 Manager 擁有。本 package 無啟動設定，nil Manager 返回 UOW_OPTIONS_INVALID。

敏感 Use Case 使用 `AuthorizedCommand(ctx, manager, authorizer, action, resourcePolicy, work)`：在建立交易前拒絕入口權限，再於 admission 後的交易內重驗 action 與 Resource Policy，直接 Application 呼叫同樣受保護。Authorizer 為中立 Port，由 constructor 注入；未設定或缺可信 Scope 預設拒絕。Feature 開啟不授權。底層 Command／Query 僅表示交易政策，應用不能把它們當作權限檢查。

驗證：`go test ./application`。背景工作持久化仍由後續交付；可靠 Consumer 使用相同交易政策與明示服務授權。
