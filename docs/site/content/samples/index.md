---
title: 套件使用範例
description: 選擇可以直接執行的 panGOlin 範例，學習各個套件的實際用法。
---

以下程式各自有完整 `main.go`，可以執行、修改，再搬到你自己的專案。

## 不需要外部服務的範例

從[安裝頁](../getting-started/installation.md)建立的 **`pangolin-work` 工作資料夾**執行：

```bash
cd pangolin-framework/examples/basics
```

**以下命令都從 `examples/basics` 執行：**

| 想學的功能 | 命令 | 預期結果 | 完整程式與說明 |
|---|---|---|---|
| 第一個 Root 裝配應用 | `go run ./hello` | `Hello, panGOlin!` | [第一個應用](../getting-started/quickstart.mdx) |
| 註冊服務、設定預設與環境覆寫 | `go run ./root-greeting` | `Hello, World!`；`APP_NAME=Reader` 後 `Hello, Reader!` | [問候服務教學](../tutorials/greeting-application.mdx) |
| 讀取預設與環境設定 | `go run ./configuration` | `Application: My App` | [設定範例](../fundamentals/configuration.mdx) |
| 多模組相依與反序停機（Root 預設） | `go run ./root-lifecycle` | 啟動 storage、books，再反序停止 | [啟停範例](../fundamentals/application-startup.mdx) |
| 判斷權限與明確拒絕 | `go run ./authorization` | read allowed、write denied | [授權範例](../security/authorization.mdx) |
| 追蹤操作與健康檢查 | `go run ./observability` | Trace correlated、livez／readyz 與 JSON log | [觀測範例](../platform/observability.mdx) |
| 建立與讀回事件訊息 | `go run ./event-envelope` | 事件 ID、型別、schema 與書籍 JSON | [事件範例](../events/integration-events.mdx) |
| 進階：明示清單組裝與 `host.Ready` | `go run ./lifecycle` | 啟動 storage、books 與 `ready: true`，再反序停止 | [啟停範例](../fundamentals/application-startup.mdx) |
| 進階：獨立 DI 容器與介面注入 | `go run ./dependency-injection` | `Hello, Alice!` | [DI 範例](../fundamentals/dependency-injection.mdx) |
| 進階：舊式 Module 與 `b.Provide` 組合 | `go run ./greeting-app` | `Hello, World!`；`APP_NAME=Reader` 後 `Hello, Reader!` | [問候服務教學](../tutorials/greeting-application.mdx) |

若你設定過 `APP_NAME`，設定／問候範例會使用該值；移除它即可看到表中的預設輸出。

## 多模組契約組合示範

`examples/moduleapp` 是 root 裝配跨模組契約的正式示範（獨立 Go module）。在套件來源的 `pangolin-framework/examples/moduleapp` 執行：

```bash
go run .
```

非互動環境可先設定 `MODULEAPP_DEADLINE=2s`。預期輸出依 Type 相依的規劃層、各模組服務診斷與反序停機；跨模組介面契約（contracts 套件與 Export）的完整檔案見[依賴注入](../fundamentals/dependency-injection.mdx)。

## 需要資料庫的交易範例

先完成 Bookstore 的服務準備，設定 PANGOLIN_DSN 後，從 `examples/basics` 執行 `go run ./transactions`。五行結果應全部為 true，依序顯示共用根、獨立根、父回滾、子提交及範圍結束。完整程式、命令與對帳方式見[交易傳播教學](../tutorials/transactions.mdx)。

## HTTP、交易與多租戶範例

| 想學的套件 | 執行中的範例 | 使用說明 |
|---|---|---|
| persistence/uow | Bookstore 建立書籍交易 | [使用交易](../data/unit-of-work.md) |
| persistence/postgres | Bookstore Repository | [資料存取](../data/postgresql.md) |
| security/authn | Bookstore Bearer Token 驗證 | [登入身份](../security/authentication.md) |
| security/scope | Bookstore Tenant 隔離 | [多租戶](../security/multi-tenancy.md) |
| events/nats | Bookstore API／Worker 的事件流程 | [Integration Event](../events/integration-events.mdx) |

[Bookstore 教學](../tutorials/bookstore.md)會帶你：

1. 準備自己的 PostgreSQL、NATS 與本機登入服務。
2. 啟動 API 與事件 Worker。
3. 取得登入 Token、建立書籍並讀回。
4. 嘗試跨租戶存取，觀察拒絕結果。

這個範例需要 Docker。它也示範 UOW 交易、同交易保存事件與背景處理；你可以依照[應用架構](../concepts/architecture.md)將自己的業務功能放入相同分層。
