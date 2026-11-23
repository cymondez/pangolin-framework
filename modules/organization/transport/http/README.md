# Organization HTTP Adapter

`NewHandler(Options)` 將公開 `contracts.Management` 與 `JoinAsOwner`／`Recover` Port 接到控制面 HTTP 路由。只有啟用 Organization Profile 才註冊本 Handler。呼叫端明示注入 JWT 驗證、Tenant／Scope Resolver、入口授權、UOW Manager、timeout、body 上限及獨立安全日誌。

入口順序為驗證身分、解析 Tenant／Scope、入口 action 授權、限制並解碼 DTO、短 UOW、Management 自身授權，最後在根交易提交後輸出 JSON。Tenant 來自可信 binding；target Kind／ID 來自路由，DTO 不接受 owner 或 Tenant 欄位。寫入要求 `Idempotency-Key`；更新要求 `expected_version`。未知欄位與缺少必要資料回 400，body 超限回 413，未知資源回 404，版本／最後 owner／非空資源衝突回 409。Commit Unknown 保留 503 與 Unknown outcome。

路由包括 `POST /enterprises`、`POST /organizations`、`GET /organizations?limit=20`、`GET /enterprises/{id}/organizations?limit=20`、`PUT|DELETE /organizations/{id}/memberships`、`PUT /enterprises/{id}/roles`、`PUT /organizations/{id}/enterprise`、`POST /organizations/{id}/join-as-owner`，以及 `/scopes/{kind}/{id}/{suspend,resume,archive,recover}`。Role／Principal 放於 membership／recovery DTO。入口 action 只是第一層檢查，不能替代 Management 的逐 target 權限。

Adapter 不擁有 pool／server 或其他可關閉資源。以 `go test ./modules/organization/transport/http` 驗證順序、DTO 負例與所有路由；真實治理交易由 `go test -tags=integration ./tests/m3 -run 'TestA8|TestA9'` 驗證。
