# Bookstore API 程序

入口為 `go run ./examples/bookstore/cmd/api -listen 127.0.0.1:18181`。先設定 PANGOLIN_DSN（app role）與 PANGOLIN_ISSUER，必要時明示 PANGOLIN_JWKS_URL；schema 必須由獨立 fixture／部署步驟建立。API 不執行 DDL。

主程序將 Ctrl+C／SIGTERM 傳給 Runtime；`-run-for 10s` 可用於有界 demo。錯誤以非零退出；成功停機先拒絕新請求、Drain，再關閉 DB。路由見 [HTTP 模組](../../transport/http/README.md)，完整設定及生命週期見 [bootstrap](../../bootstrap/README.md)。

驗證由 [M1 指南](../../../../docs/guides/m1-development.md) 的插樁分程序 E2E 執行，不能只以 main 可編譯當成 API 驗收。
