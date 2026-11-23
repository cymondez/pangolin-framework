# transport/http

共用入口機制，不認識 Bookstore 模型。先在外層驗證身分、Tenant、Scope 與入口 Policy，再使用 `Command(manager, callback)`。它擁有一個頂層可寫交易，僅在 Within 確認提交後序列化並寫 2xx；傳入已有 UOW 的 context 會拒絕，避免 Required 子範圍提早回覆。

`WriteError` 輸出安全 Problem Details。Unknown 優先於 timeout，503 附 `outcome=unknown` 與 operationId，不提供無條件重試保證。提交後序列化失敗為 500／committed；傳輸失敗保留 operationId 日誌，不改寫 DB 結果。

串流連線使用沒有 UOW 的 context；每次資料庫工作各呼叫 `StreamOperation`，操作結束立即釋放交易，不能返回 cursor。寫入串流操作可在自己的 Application Command 內建立短範圍，另以協定確認結果。完整授權、重播及冪等仍由應用政策處理。

`NewPipeline(PipelineOptions)` 將 Authenticate → ResolveTenant → ResolveScope → Entry → body 大小／Decode → Within → ResourcePolicy → Work 固定為可驗證順序。Timeout、MaxBodyBytes 及所有必要 Port 啟動時必填；每階段保留已驗證的 context。拒絕與業務回滾後的 Rejected callback 走獨立安全日誌，callback 故障不改寫交易結果。查詢使用短唯讀 UOW，寫入使用 Command；body DTO 的 owner 必須另以 `scope.MatchOwner` 驗證，不能由 body 決定授權。

提交後自訂 MarshalJSON 返回錯誤或 panic 都保留 committed／operationId，Outcome Unknown 優先於任何一般錯誤分類。可識別安全錯誤與支援 `HTTPStatus()/Code()` 的已分類錯誤映射為安全 Problem Details，不直接輸出 cause。

資源與 CleanupTimeout／HookTimeout 由 UOW Manager 管理，本 package 不關閉 DB。驗證：`go test ./transport/http`、`go test -tags=integration ./tests/m2 ./tests/m3`。
