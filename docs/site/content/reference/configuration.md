---
title: Bookstore 設定參考
description: 框架內建 Bookstore 範例 API、背景 Worker 的環境變數、命令列旗標，以及 Jobs 與觀測的起始調校建議。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

> **適用範圍提醒**：本頁設定專屬於框架原始碼內建的 `examples/bookstore`（見 [Bookstore 教學](../tutorials/bookstore.md)）。如果你使用的是 **CLI 生成的獨立專案**（如 `PANGOLIN_MIGRATION_DSN`、`PANGOLIN_API_PORT`），請改查 **[pangolin CLI 參考](cli.md)**。

API 與 Worker 是兩個獨立程序，請在各自執行的終端機或容器環境設定對應變數。

---

## 1. 環境變數一覽

| 環境變數 | 所屬程序 | 必填 | 用途與教學範例值 |
|---|---|---|---|
| `PANGOLIN_DSN` | API、Worker | 是 | 各自最小權限角色的 PostgreSQL 連線字串（API 用 `pangolin_app`、Worker 用 `pangolin_worker`） |
| `PANGOLIN_ISSUER` | API | 是 | 可信 JWT issuer（教學：`http://127.0.0.1:18180`） |
| `PANGOLIN_JWKS_URL` | API | 否 | 覆寫 JWKS 公鑰位址；省略時預設為 `${PANGOLIN_ISSUER}/jwks.json` |
| `PANGOLIN_NATS_URL` | Worker | 是 | NATS 服務連線位址（教學：`nats://127.0.0.1:14322`） |
| `PANGOLIN_NATS_STREAM` | Worker | 是 | JetStream 串流名稱（教學：`BOOKSTORE_DEMO`） |
| `PANGOLIN_NATS_SUBJECT` | Worker | 是 | 訂閱 Subject（教學：`bookstore.demo.book_created`） |
| `PANGOLIN_WORKER_TENANTS` | Worker | 是 | 負責處理的租戶清單，以逗號分隔（教學：`tenant-a,tenant-b`） |

> **安全提醒**：Bookstore 範例的 JWT audience 固定為 `pangolin-bookstore`。所有資料庫密碼與機密應由受信任的環境或密鑰服務注入，絕不放在命令列參數或日誌中。

---

## 2. 命令列旗標（Flags）

| Flag | 預設值 | 用途 |
|---|---|---|
| `-listen` | `127.0.0.1:18081` | API HTTP 監聽位址（教學中刻意改為 `127.0.0.1:18181` 以免衝突） |
| `-run-for` | `0`（持續運行） | 設定自動優雅停機期限（例如 `-run-for 10s`，常用於自動化驗證） |

範例（在已設好 API 環境變數的 `pangolin-framework` 根目錄執行）：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
go run ./examples/bookstore/cmd/api -listen 127.0.0.1:18181 -run-for 10s
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
go run ./examples/bookstore/cmd/api -listen 127.0.0.1:18181 -run-for 10s
```

</TabItem>
</Tabs>

---

## 3. 在自己的應用設計組態與預設上限

你自己的專案**完全不需要**照抄 Bookstore 的環境變數名稱，請依[應用設定](../fundamentals/configuration.mdx)用 `config.Source` 自由映射並加上驗證規則。

若專案啟用了持久化 Jobs 與 OpenTelemetry 觀測，可採用以下經實測的**推薦起始基準**，再依實際流量調整：
- **Jobs Policy 起始值**（詳見[持久化背景工作](../platform/jobs.md)）：`Lease=30s`、`ExecutionTimeout=20s`、`CleanupTimeout=5s`、`PollInterval=100ms`、`Concurrency=4`、`BatchSize=32`、`MaxAttempts=8`、`Retry=250ms–30s`、`Jitter=0.2`、`MaxPending=100000`、`Retention=720h`。
- **觀測與健康檢查起始值**（詳見[觀測與健康檢查](../platform/observability.mdx)）：由應用明確注入 OpenTelemetry Provider 與 Exporter，明示取樣率與保留期；Health 檢查逾時可從 `2s` 起步，定期上報 Queue Stats 與 Pool 使用率。
