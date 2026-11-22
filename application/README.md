# application

`Command(ctx, manager, callback)` 明示 ReadWrite／Transactional；`Query` 明示 ReadOnly。HTTP、Job、Consumer 呼叫同一 Application Use Case，入口方法或反射不會改寫交易政策。直接使用 Manager 的零值選項仍按規範預設 ReadOnly。

callback 僅使用傳入的 context。返回 nil 是要求完成，仍可能因 Commit／取消／rollback-only 失敗；呼叫方保留 cause 與 Outcome，不自行提交或重跑。資源與清理由 Manager 擁有。本 package 無啟動設定，nil Manager 返回 UOW_OPTIONS_INVALID。

驗證：`go test ./application`。背景工作持久化與完整可靠 Consumer 仍按各自里程碑交付；本 package 僅提供共用呼叫邊界。
