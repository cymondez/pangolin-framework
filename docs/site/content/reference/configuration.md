---
title: Bookstore 設定參考
description: 調整 Bookstore API、Worker 的連線與執行選項。
---

完成 [Bookstore 教學](../tutorials/bookstore.md)後，可以依需求調整以下設定。API 與 Worker 是不同程序，請在各自終端設定環境變數。

## 環境變數

| 名稱 | 程序 | 用途 |
|---|---|---|
| `PANGOLIN_DSN` | API、Worker | 各自資料庫角色的 PostgreSQL DSN，必填 |
| `PANGOLIN_ISSUER` | API | 可信 JWT issuer，必填 |
| `PANGOLIN_JWKS_URL` | API | 預設為 issuer 下的 `/jwks.json` |
| `PANGOLIN_NATS_URL` | Worker | NATS 服務 URL，必填 |
| `PANGOLIN_NATS_STREAM` | Worker | 事件 stream；教學使用 `BOOKSTORE_DEMO` |
| `PANGOLIN_NATS_SUBJECT` | Worker | 事件 subject；教學使用 `bookstore.demo.book_created` |
| `PANGOLIN_WORKER_TENANTS` | Worker | 服務負責的 Tenant 清單，以逗號分隔，必填 |

JWT audience 為 `pangolin-bookstore`。請將密碼與 Token 放在可信環境，避免出現在 CLI flags 或記錄。

## 程序選項

| Flag | 預設 | 用途 |
|---|---|---|
| `-listen` | `127.0.0.1:18081` | API 監聽位置，教學明示改為 18181 |
| `-run-for` | `0` | 例如 `10s` 可在期限後正常停機；0 持續執行 |

範例：在已設定 API 環境的套件來源根目錄執行 `go run ./examples/bookstore/cmd/api -listen 127.0.0.1:18181 -run-for 10s`。

## 自己的應用設定

你的應用不必使用 Bookstore 的環境變數名稱。使用 [Configuration](../fundamentals/configuration.mdx)將你選擇的名稱映射成 Source.Values，並提供必要欄位與格式驗證。
