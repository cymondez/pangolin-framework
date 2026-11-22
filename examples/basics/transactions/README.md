# Required 與 RequiresNew

先依 [Bookstore 教學](../../../docs/site/content/tutorials/bookstore.md) 準備 disposable PostgreSQL；本例只需要資料庫，不需要保留 API／Worker 程序。

從 `examples/basics` 執行：

```powershell
$env:PANGOLIN_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
go run ./transactions
```

預期五行皆為 true：Required shares root、RequiresNew has new root、Parent rolled back、Child committed、Scope closed。例子使用真實獨立交易對帳，子提交會留下 tenant-a 的一筆書籍，父資料回滾。每次使用新 ID；清除自己的 demo volumes 可清除示範資料。

Manager 管理交易與清理，Pool 由 main 關閉；初始 DefaultTimeout／CleanupTimeout／HookTimeout／MaxRequiresNewDepth 都明示。固定身分僅用於本機命令列教學，API 必須改用已驗證身分的 IdentityReader。缺少連線設定或資料表時以錯誤結束，不模擬成功。

完整程式與解說見 [交易傳播教學](../../../docs/site/content/tutorials/transactions.mdx)。驗證：從根目錄設定 PANGOLIN_DSN 後執行 `python scripts/docs_examples.py --database`。
