# Bookstore 專用故障與對帳程序

只供分程序 E2E 使用，production API／Worker 不會 import 本目錄。共享的 IdP 留在 repository root 的 internal/testkit。

| 入口 | 行為 |
|---|---|
| `cmd/crash-worker` | 注入 Broker decorator，在真實 publish ACK 後、mark sent 前以 exit 17 結束；驗證重啟同 EventID 配送 |
| `cmd/m1-query` | 使用 disposable fixture 的 admin DSN，以唯讀交易查詢標量結果，供 E2E 核對 Book／Outbox／Inbox／Read Model |

由 `scripts/m1_e2e.py` 建置至 artifacts，再以獨立程序執行。兩個 main.go 在 coverage manifest 逐檔列為 fixture，不替代正式 API／Worker 的插樁 coverage；手寫範例程式仍完整計入。實際操作由 [M1 指南](../../../../docs/guides/m1-development.md) 的完整 Gate 啟動，不應部署這些程序。
