---
title: 建立專案：pangolin new
description: 使用 pangolin new 建立 tenant-only、organization-enabled 或含 SignalR 即時通訊的獨立 Go 專案，檢視目錄結構與完整啟動流程。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

`pangolin new` 會在**目前工作目錄下**建立一個全新、可獨立編譯與測試的 Go 模組化單體專案，並內附與 CLI 同源版本（`0.1.0`）的 `framework/` Runtime 與 `runtime-manifest.json` 雜湊清單。

> **工作目錄提醒**：請先切換到目標專案的**上層資料夾**再執行 `pangolin new`。

---

## 1. 語法與旗標說明

```text
pangolin new NAME --template modular-monolith [--with organization] [--realtime]
```

| 參數／旗標 | 必填 | 預設值 | 合法值與說明 |
|---|---|---|---|
| `NAME` | 是（位置參數，須在 flags 之前） | 無 | 專案資料夾名稱與 Go module 尾段（`example.com/<NAME>`）。須符合 `^[a-z][a-z0-9_-]{0,47}$` |
| `--template` | 否 | `modular-monolith` | 專案範本，目前僅支援 `modular-monolith` |
| `--with` | 否 | 空（`tenant-only`） | 傳入 `organization` 時切換為 `organization-enabled` Profile |
| `--realtime` | 否 | `false` | 額外加入可選 SignalR 即時通訊模組（`/hub`、`/hub/negotiate` 與 `REALTIME.md`），可與預設或 `--with organization` 併用 |

---

## 2. 三種 Profile／能力組合對照

| 組合 | 執行指令 | `pangolin.json` 記錄 | 實際包含的能力與差異 |
|---|---|---|---|
| **Tenant-only（預設）** | `pangolin new bookstore --template modular-monolith` | `"profile": "tenant-only"` | 僅包含 `persistence`、`authorization`、`bookstore`。**不引入任何組織套件、Schema 或治理路由**，資料精確歸屬 `tenant/<TenantID>`。 |
| **Organization-enabled** | `pangolin new bookstore-org --template modular-monolith --with organization` | `"profile": "organization-enabled"` | 額外包含完整 `modules/organization` 模組、Enterprise／Organization 治理 HTTP 路由、ScopeAware Schema Migration 與對應 OpenAPI 規格。 |
| **啟用 SignalR 即時通訊** | 上述任一指令加上 `--realtime` | `"realtime": true`、模組清單加入 `realtime` | 額外掛載 `modules/realtime`（`/hub`、`/hub/negotiate`）、JetStream 接收器接線與 `REALTIME.md` 設定指引（注意：API 不含 Outbox Worker，需依 `REALTIME.md` 另啟派送程序）。 |

> **不可只改 `pangolin.json` 切換 Profile**：`tenant-only` 與 `organization-enabled` 擁有不同的相依圖與資料庫 Schema，且 API 啟動時會核對資料庫 `bookstore.profile`；既有專案升級組織須依[部署資料庫遷移](../data/migrations.md)執行前向遷移。

---

## 3. 生成專案目錄結構

以 `pangolin new bookstore --template modular-monolith` 為例，產出的目錄結構如下：

```text
bookstore/
├── pangolin.json               # 專案中繼資料（name、version、profile、realtime、modules）
├── runtime-manifest.json       # framework/ 下所有受管檔案的 SHA-256 雜湊清單
├── go.mod / go.sum             # 以本機 replace 指向 ./framework
├── compose.yaml                # 本機 Docker Compose（postgres、idp、migrate、api）
├── Dockerfile / .dockerignore  # 多二進位檔建置（api、migrate、devidp）
├── README.md / PROFILE.md      # 專案與當前 Profile 說明（--realtime 另含 REALTIME.md）
├── cmd/
│   ├── api/main.go             # API 入口（啟動前驗證 Schema 與 Profile 相容性）
│   ├── migrate/main.go         # 部署專用資料庫遷移入口（支援 --seed）
│   └── devidp/main.go          # 本機開發用無驗證 RS256 IdP（切勿用於正式環境）
├── deploy/init/001-roles.sql   # 本機資料庫最小權限角色初始化（app / worker / owner）
├── foundation/                 # 平台基礎模組：擁有 DB/UOW/Scope/Outbox 並 Export 公開契約
├── internal/
│   ├── app/                    # 應用根模組（HTTP 路由、Drain Participant、Migration 組裝）
│   ├── domain/                 # 純 Go 領域模型與單元測試
│   ├── application/            # Use Case（使用 application.Runner 與明確交易政策）
│   └── infrastructure/         # Repository 與 SQL 查詢
├── modules/
│   └── registry.go             # CLI 管理的 UserRoot 聚合檔（供 pangolin module add 更新）
├── contracts/runtime_test.go   # 生成專案契約測試
└── framework/                  # 與 CLI 同版的內附 Runtime 原始碼（由 pangolin upgrade 管理）
```

---

## 4. 雙環境完整建立到啟動驗證流程

以下示範從框架根目錄建置 CLI、建立專案、跑過單元測試、透過 Docker Compose 啟動並檢查 `/readyz`：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 1. 在 pangolin-framework 根目錄建置 CLI
go build -o .cache/bin/pangolin.exe ./cmd/pangolin
$pangolin = (Resolve-Path .cache/bin/pangolin.exe).Path

# 2. 切到上層目錄建立專案（可依需求加上 --with organization 或 --realtime）
cd ..
& $pangolin new bookstore --template modular-monolith
cd bookstore

# 3. 驗證編譯與單元測試
go test ./...

# 4. 透過 Docker Compose 啟動（PostgreSQL -> IdP -> migrate --seed -> API）
docker compose up -d --build --wait
(Invoke-WebRequest http://localhost:18281/readyz).StatusCode
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 1. 在 pangolin-framework 根目錄建置 CLI
go build -o .cache/bin/pangolin ./cmd/pangolin
PANGOLIN="$(pwd)/.cache/bin/pangolin"

# 2. 切到上層目錄建立專案（可依需求加上 --with organization 或 --realtime）
cd ..
"$PANGOLIN" new bookstore --template modular-monolith
cd bookstore

# 3. 驗證編譯與單元測試
go test ./...

# 4. 透過 Docker Compose 啟動（PostgreSQL -> IdP -> migrate --seed -> API）
docker compose up -d --build --wait
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:18281/readyz
```

</TabItem>
</Tabs>

### 預期輸出

- `pangolin new` 成功時印出：
  ```text
  Created bookstore profile tenant-only runtime 0.1.0
  ```
  （加上 `--with organization` 則為 `Created bookstore-org profile organization-enabled runtime 0.1.0`）
- `/readyz` 健康檢查回傳 HTTP **`204`**。

### 同時啟動多個生成專案（自訂 Port）

預設 Compose 使用本機 `15642`（PostgreSQL）、`18280`（IdP）、`18281`（API）。要同時啟動第二個專案（例如 `bookstore-org`），只需覆寫三個 Port 環境變數：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
$env:PANGOLIN_PG_PORT = '15643'
$env:PANGOLIN_IDP_PORT = '18290'
$env:PANGOLIN_API_PORT = '18291'
docker compose up -d --build --wait
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
PANGOLIN_PG_PORT=15643 PANGOLIN_IDP_PORT=18290 PANGOLIN_API_PORT=18291 \
  docker compose up -d --build --wait
```

</TabItem>
</Tabs>

---

## 5. 常見錯誤與處置

| 錯誤訊息 | 原因 | 處置方式 |
|---|---|---|
| `project name must start with a lowercase letter and contain lowercase letters, digits, underscore or hyphen` | 名稱含大寫字母、特殊符號、數字開頭或超過 48 字元 | 改用合法小寫名稱（例如 `my-store` 或 `bookstore_org`） |
| `unsupported template or module option` | `--template` 不是 `modular-monolith`、`--with` 不是 `organization`，或夾帶多餘參數 | 檢查旗標拼字並確保 `NAME` 位於所有 flags 之前 |
| `target already exists; no files changed` | 目標資料夾（或同名檔案）已存在 | 改用新名稱或先移走既有目錄；CLI 不會覆寫既有路徑 |

## 下一步

專案建立並跑通後，接著學習用 **[新增模組：`pangolin module add`](cli-module.md)** 加入第一個自己的業務模組。
