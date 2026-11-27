---
title: pangolin CLI 參考
description: pangolin CLI 建置方式、工作目錄規則、子命令導航、全域命名與安全防護、完整環境變數與範例差異總覽。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

> **前置操作**：第一次使用 CLI 時，建議先跟著實作一次 [用 CLI 建立自己的 Bookstore](../tutorials/generated-bookstore.md)。本版本 CLI 與生成 Runtime 版本均為 `0.1.0`。

---

## 1. 安裝並驗證 `pangolin` CLI

透過標準 `go install` 安裝 `v0.1.0` 版本的 `pangolin` CLI，並確認版本輸出為 `0.1.0`：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 安裝 v0.1.0 CLI（請確認 "$(go env GOPATH)\bin" 已加入 PATH）
go install github.com/pangolin-framework/pangolin/cmd/pangolin@v0.1.0

# 驗證版本與說明
pangolin --version
pangolin --help
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 安裝 v0.1.0 CLI（請確認 "$(go env GOPATH)/bin" 已加入 PATH）
go install github.com/pangolin-framework/pangolin/cmd/pangolin@v0.1.0

# 驗證版本與說明
pangolin --version
pangolin --help
```

</TabItem>
</Tabs>

也可以直接從 [GitHub Releases](https://github.com/pangolin-framework/pangolin/releases/tag/v0.1.0) 下載對應作業系統的預編譯執行檔並放入 `PATH`；若需要從原始碼自行建置，可在 `pangolin` 倉庫根目錄執行 `go install ./cmd/pangolin`。以下各子頁皆直接使用 `pangolin` 指令示範。

---

## 2. 工作目錄規則

執行位置只有一條核心原則：

- **`pangolin new`**：必須在「目標專案目錄的**上層資料夾**」執行（會在目前目錄下新建 `<name>/`）。
- **其餘所有子命令**（`module add`、`migrate`、`doctor`、`generate openapi`、`upgrade`）：一律在「**生成專案的根目錄**」（包含一般檔案 `pangolin.json` 的目錄）執行。

若在非專案目錄（或 `pangolin.json` 非一般檔案、版本與 CLI 不符）執行專案內命令，CLI 會立即中止並回傳非零結束碼。

---

## 3. 子命令快速導航

每個命令皆支援 `--help`（例如 `pangolin new --help`、`pangolin module add --help`、`pangolin upgrade --help`）；失敗一律回傳非零結束碼。

| 子命令分類 | 核心指令 | 詳細手冊 |
|---|---|---|
| **建立專案** | `pangolin new NAME --template modular-monolith [--with organization] [--realtime]` | [建立專案：`pangolin new`](cli-new.md) |
| **新增業務模組** | `pangolin module add NAME [--depends ...] [--preset service]` | [新增模組：`pangolin module add`](cli-module.md) |
| **資料庫遷移與健檢** | `pangolin migrate create`、`pangolin migrate status`、`go run ./cmd/migrate`、`pangolin doctor [--live]` | [遷移與健檢：`migrate` 與 `doctor`](cli-migrate-doctor.md) |
| **規格輸出與 Runtime 升級** | `pangolin generate openapi`、`pangolin upgrade plan \| --dry-run \| runtime \| verify` | [規格與升級：`openapi` 與 `upgrade`](cli-openapi-upgrade.md) |
| **版本與說明** | `pangolin --version`、`pangolin --help` | 本頁 |

---

## 4. 全域命名、旗標與安全防護規則

### 命名與參數順序規則

- **參數位置**：名稱參數（`NAME`）必須放在 flags **之前**，後續 flags 可任意順序排列。
- **專案名稱**（`new NAME`）：正則 `^[a-z][a-z0-9_-]{0,47}$`（以小寫英文字母開頭，最長 48 字元，僅允許小寫字母、數字、`_`、`-`）。
- **模組與遷移名稱**（`module add NAME`、`migrate create NAME`）：正則 `^[a-z][a-z0-9_]{0,47}$`（以小寫英文字母開頭，最長 48 字元，僅允許小寫字母、數字與 `_`；同時拒絕所有 Go 語言保留字與 `main`）。
- **嚴格參數驗證**：任何未知 flag、多餘位置參數、未知 preset 或 `--preset crud`（目前明示不支援）一律直接拒絕，不產生半成品檔案。

### 原子寫入與檔案防護

- **原子 staging**：`new` 與 `module add` 皆先在暫存目錄完整產生所有檔案，確認無誤後才一次 rename 落地；中途失敗會自動清理暫存並回滾，絕不留下殘缺目錄。
- **併發鎖保護**：`module add` 與 `upgrade runtime` 會建立 `.pangolin-module.lock` 防止同目錄並發修改專案檔案。
- **不覆寫使用者修改**：
  - `new` 拒絕已存在的目標目錄（`target already exists; no files changed`）。
  - `module add` 拒絕重複模組名稱、已存在目錄，以及被手動編輯過的 `modules/registry.go`（`module registry was edited; existing files preserved`）。
  - `migrate create` 拒絕同模組內已存在同名後綴的遷移檔。
  - `generate openapi` 內容與範本一致時可安全重入；若有差異則保留原檔並報錯。
- **路徑與符號連結防護**：拒絕逃逸出專案根目錄的路徑（`..`），並拒絕透過符號連結（symlink）寫入受管檔案。

---

## 5. 完整環境變數一覽

CLI 生成專案與開發命令所使用的環境變數整理如下。**敏感連線字串（DSN）與憑證密鑰一律透過環境變數注入，切勿放進 CLI 參數或日誌。**

| 環境變數 | 適用對象 | 預設值／格式 | 說明 |
|---|---|---|---|
| `PANGOLIN_MIGRATION_DSN` | `cmd/migrate`、`migrate status`、`doctor --live` | 無（必填） | 具備 DDL 與 Goose 版本表權限的部署資料庫連線字串 |
| `PANGOLIN_DSN` | 生成 API、`doctor --live` | 無（必填） | 受 RLS 保護的最小權限應用角色（`pangolin_app`）連線字串 |
| `PANGOLIN_AUTHORITY_DSN` | 生成 API | 無（必填） | 查詢當前 Tenant／組織資格與事件儲存的 metadata 帳號（`pangolin_worker`） |
| `PANGOLIN_ISSUER` | 生成 API、`devidp` | Compose 預設 `http://localhost:${PANGOLIN_IDP_PORT:-18280}` | 可信 JWT issuer |
| `PANGOLIN_JWKS_URL` | 生成 API | Compose 預設 `http://idp:8080/jwks.json` | 驗證 JWT RS256 簽章用的公鑰 JWKS 位址 |
| `PANGOLIN_LISTEN` | 生成 API（原生執行） | `127.0.0.1:18081`（Compose 容器內為 `0.0.0.0:8080`） | HTTP 伺服器監聽位址 |
| `PANGOLIN_API_URL` | `doctor --live` | 未設時自動組為 `http://127.0.0.1:${PANGOLIN_API_PORT:-18281}` | 指定 `/livez` 與 `/readyz` 健康檢查目標位址 |
| `PANGOLIN_PG_PORT` | 生成的 `compose.yaml` | `15642` | PostgreSQL 本機 loopback port |
| `PANGOLIN_IDP_PORT` | 生成的 `compose.yaml` | `18280` | 本機開發 IdP loopback port |
| `PANGOLIN_API_PORT` | 生成的 `compose.yaml`、`doctor --live` | `18281` | 生成 API 本機 loopback port |
| `PANGOLIN_REALTIME_NATS_URL` | `--realtime` API | 無（啟用即時模組時必填） | NATS JetStream 連線位址 |
| `PANGOLIN_REALTIME_STREAM` | `--realtime` API | 無（啟用即時模組時必填） | JetStream limits-retention file stream 名稱 |
| `PANGOLIN_REALTIME_SUBJECT` | `--realtime` API | 無（啟用即時模組時必填） | 專用 literal subject |
| `PANGOLIN_REALTIME_INTROSPECTION_URL` | `--realtime` API | 無（啟用即時模組時必填） | HTTPS 憑證 introspection 端點 |
| `PANGOLIN_REALTIME_INTROSPECTION_ID` / `PANGOLIN_REALTIME_INTROSPECTION_SECRET` | `--realtime` API | 無（啟用即時模組時必填） | Introspection client 憑證 |
| `PANGOLIN_REALTIME_ORIGINS` | `--realtime` API | 空（同源） | 跨來源瀏覽器部署白名單（以逗號分隔） |

---

## 6. 與框架內建 `examples/bookstore` 的差異對照

避免混淆「CLI 生成的獨立專案」與「框架倉庫內的 `examples/bookstore` 範例」：

| 比較項目 | CLI 生成的專案（`pangolin new`） | 框架內建範例（[Bookstore API 參考](bookstore-api.md)） |
|---|---|---|
| `POST /books` 成功狀態 | **HTTP 200**（必須帶 `Idempotency-Key` 標頭） | **HTTP 201** |
| 書籍回應欄位 | `id`、`title` | `id`、`title`、`version` |
| 內建服務結構 | 單一 Compose 含 `postgres`、`idp`、`migrate`、`api`（Outbox Worker 依需求另掛） | 獨立 `examples/bookstore/cmd/api` 與 `cmd/worker` 雙程序 + NATS |
| 組織治理支援 | `--with organization` 內建完整治理控制平面與 OpenAPI | 預設為 `tenant-only` 示範 |
| 即時通訊支援 | `--realtime` 內建 `/hub` 與 `/hub/negotiate` SignalR 端點 | 另由 `examples/realtime` 獨立示範 |
