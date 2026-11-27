# Tenant 與 Permission 授權

本例使用公開 API 串接 `tenancy.Resolver` → `scope.Registry` → `policy.Require`，不需要 PostgreSQL、Broker 或外部 IdP。

前置條件是倉庫指定版本的 Go，以及本機 Framework checkout。`examples/basics` 是既有的獨立使用者 module，透過 `require`／本機 `replace` 使用 Framework。從倉庫根目錄執行：

```powershell
Set-Location examples/basics
go run ./authorization
```

預期輸出：

```text
read allowed
write denied
feature enabled: write denied
```

完整程式位於 [main.go](main.go)。`demoIdentity` 與 `demoAuthority` 是明示的教學 fixture：前者提供固定的 Alice／Tenant，後者只允許這份成員資格。正式應用應改用驗證 JWT 的身分讀取器與目前有效的 Tenant 資格來源。這個程式示範權限組裝；沒有建立交易或執行資料庫驗收。

程式先核對路由／Header 的 Tenant 候選值與成員資格，再綁定 Tenant Scope。`books.read` 的角色允許會通過；`books.write` 雖然同時有 Role、Tenant 與 System 允許，仍因 User 明確 deny 而拒絕。未知 Permission 沒有允許規則，預設拒絕。Feature 只控制功能可用性，開啟後仍需通過相同 `policy.Require`。

敏感 Application Use Case 應保留自身的 `Require`，HTTP 入口檢查不代替 Use Case 授權。需要依資源內容判斷時，再提供 `ResourcePolicy`；本例使用 `nil`，因為沒有讀寫任何業務資源。下一步可閱讀 [交易傳播範例](../transactions/README.md)，將已授權的操作接入真實工作單元。

驗證方式：從倉庫根目錄執行 `python scripts/docs_examples.py`。工具會建立另一個獨立 consumer module、複製完整程式並核對上述三行輸出。
