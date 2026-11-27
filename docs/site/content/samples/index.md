---
title: 套件使用範例總覽
description: 依學習階段分類的所有可執行 panGOlin 範例程式，每個範例皆附有中文導讀註解、執行命令與預期輸出。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

這一頁彙整了 panGOlin 所有可直接執行的範例程式。你可以先從**無需啟動外部服務**的基礎範例跑起，確認行為符合預期後，再直接參考或搬移到自己的專案。

> **取得範例倉庫**：如果尚未下載範例原始碼，先執行 `git clone --branch v0.1.0 https://github.com/pangolin-framework/pangolin.git`。除非特別標示，基礎範例一律從 `pangolin/examples/basics` 目錄執行：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
cd pangolin/examples/basics
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
cd pangolin/examples/basics
```

</TabItem>
</Tabs>

---

## 階段 1：免外部服務範例（只需 Go）

以下範例不需要 Docker、資料庫或任何外部服務，全部都能在幾秒內跑出明確結果：

| 學習目標 | 執行命令 | 預期輸出摘要 | 對應教學指南 |
|---|---|---|---|
| 第一個 Root 裝配應用與優雅停機 | `go run ./hello` | `Hello, panGOlin!` | [第一個應用](../getting-started/quickstart.mdx) |
| 註冊 DI 服務、預設設定與環境變數覆寫 | `go run ./root-greeting` | `Hello, World!`（設 `APP_NAME=Reader` 變 `Hello, Reader!`） | [問候服務教學](../tutorials/greeting-application.mdx) |
| 讀取設定、優先序覆寫與必填驗證 | `go run ./configuration` | `Application: My App` | [應用設定](../fundamentals/configuration.mdx) |
| 多模組 Type 相依排序與反序停機 | `go run ./root-lifecycle` | 啟動 `storage` → `books`，反序停止 `books` → `storage` | [應用啟動與停止](../fundamentals/application-startup.mdx) |
| 獨立低階 DI 容器（`di.NewRegistry`／`Seal`） | `go run ./dependency-injection` | `Hello, Alice!` | [依賴注入](../fundamentals/dependency-injection.mdx) |
| 權限判斷、User 明確 Deny 與 Feature 不授權 | `go run ./authorization` | `read allowed`、`write denied`、`feature enabled: write denied` | [權限與資源政策](../security/authorization.mdx) |
| 分層 Settings（5–7 層）、配額與 Scoped Cache | `go run ./platform-services` | 8 行驗證輸出（含租戶快取隔離、版本收斂等） | [分層設定與 Features](../platform/settings.mdx) |
| 建立與反序列化版本化 Event Envelope | `go run ./event-envelope` | 事件 ID、型別、Schema 版本與書籍 JSON | [事件格式與設計](../events/integration-events.mdx) |
| OpenTelemetry Trace、JSON Log 與 `/livez`／`/readyz` | `go run ./observability` | `Trace correlated: true`、`/livez: 204`、`/readyz: 204` | [觀測與 Health](../platform/observability.mdx) |

> **小提醒**：如果你在終端機設定過 `APP_NAME` 環境變數，設定與問候範例會改印該值；移除該環境變數即可恢復表中的預設輸出。

---

## 階段 2：多模組介面契約示範（獨立 Go Module，不需外部服務）

`examples/moduleapp` 示範真實專案如何把應用拆成 `store`、`catalog`、`pricing`、`app` 四個模組，並由契約擁有者把公開介面獨立在 `catalog/contracts` 套件內 `Export`，同時保護非公開服務不被跨模組存取。

在範例倉庫的 **`pangolin/examples/moduleapp`** 目錄執行：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 非互動環境建議加上 2 秒自動結束
$env:MODULEAPP_DEADLINE = '2s'
go run .
Remove-Item Env:MODULEAPP_DEADLINE
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 非互動環境建議加上 2 秒自動結束
MODULEAPP_DEADLINE=2s go run .
```

</TabItem>
</Tabs>

完整原始碼與跨模組可見性規則解析見 **[依賴注入：跨模組介面契約](../fundamentals/dependency-injection.mdx)**。

---

## 階段 3：真實資料庫與完整系統範例（需要 Docker）

當你準備好體驗真實 PostgreSQL 交易、JWT 登入、RLS 多租戶隔離與 NATS 背景事件時，依序操作以下兩項：

1. **[Bookstore 完整 API 與事件 Worker](../tutorials/bookstore.md)**
   - 啟動隔離的 PostgreSQL、NATS 與本機示範 IdP。
   - 實測登入取 Token、建立書籍（HTTP 201）、跨租戶存取被拒（HTTP 403）與背景 Outbox／Inbox 投影。
   - 各核心套件對應位置：
     - `persistence/uow` → [使用交易與工作單元](../data/unit-of-work.md)
     - `persistence/postgres` → [PostgreSQL Adapter](../data/postgresql.md)
     - `security/authn` / `security/identity` → [可擴充登入與身分驗證](../security/authentication.md)
     - `security/scope` / `tenancy` → [多租戶與資料隔離](../security/multi-tenancy.md)
     - `events/outbox` / `events/inbox` / `events/nats` → [組裝可靠事件配送](../events/reliable-delivery.md)
2. **[交易傳播實作（`go run ./transactions`）](../tutorials/transactions.mdx)**
   - 沿用 Bookstore 的示範 PostgreSQL，設定 `PANGOLIN_DSN` 後從 `examples/basics` 執行 `go run ./transactions`。
   - 印出 5 行 `true`，實際驗證 `Required` 共根、`RequiresNew` 獨立提交、父回滾不影響子提交與舊 Scope 失效。
3. **[用 CLI 生成獨立專案](../tutorials/generated-bookstore.md)**
   - 學會使用 `pangolin new` 產出專屬於你自己的專案骨架。
