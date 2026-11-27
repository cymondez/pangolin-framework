---
title: 用 CLI 建立自己的 Bookstore
description: 使用 pangolin CLI 一鍵生成獨立 Go 專案（支援 tenant-only 與 organization 兩種 Profile），學會新增可執行測試的業務模組、產生 OpenAPI 與執行 doctor 診斷。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

> **你在架構中的位置**：**`[ 專案導入起點：pangolin CLI 生成獨立專案 → Docker 啟動驗證 → module add 擴充業務模組 → doctor 健檢 ]`**
>
> **前置建議**：想把 panGOlin 正式導入新專案時，最推薦從這一頁開始。本頁示範的是 **CLI 生成的獨立專案**（建立與讀取皆回 **HTTP 200**、欄位為 `id`/`title`），與套件內既有 `examples/bookstore`（201 + `version`）不同。

## 開始前準備

- **Go 1.27.4**、**Git**、**Docker Linux Engine／Compose**。
- **終端機環境**：Windows 使用 **PowerShell**；Linux／macOS 使用 **Bash** 與 `curl`。
- 確認本機 loopback ports **`15642`、`18280`、`18281`** 未被其他服務占用。
- 預覽版只需從框架原始碼建置 `cmd/pangolin`，不需要 Node.js 或 Python。

---

## 1. 建置 CLI 並生成獨立專案（Tenant-only 預設 Profile）

在 **`pangolin-framework` 根目錄**執行：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 步驟 1：建置 pangolin CLI 可執行檔
go build -o .cache/bin/pangolin.exe ./cmd/pangolin
$pangolin = (Resolve-Path .cache/bin/pangolin.exe).Path

# 步驟 2：切到上層目錄，生成與框架並排的獨立專案 bookstore
cd ..
& $pangolin new bookstore --template modular-monolith
cd bookstore

# 步驟 3：立刻執行單元測試，確認生成專案乾淨可編譯
go test ./...
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 步驟 1：建置 pangolin CLI 可執行檔
go build -o .cache/bin/pangolin ./cmd/pangolin
PANGOLIN="$(pwd)/.cache/bin/pangolin"

# 步驟 2：切到上層目錄，生成與框架並排的獨立專案 bookstore
cd ..
"$PANGOLIN" new bookstore --template modular-monolith
cd bookstore

# 步驟 3：立刻執行單元測試，確認生成專案乾淨可編譯
go test ./...
```

</TabItem>
</Tabs>

預期看到 `Created bookstore profile tenant-only runtime 0.1.0` 與通過的測試結果。接下來的所有指令都在你自己的 **`bookstore`** 目錄執行。

> **預覽版 Runtime Bundle 說明**：因為 0.1.0 尚未發布遠端 Go tag，生成的專案會內附與 CLI 同版的 `framework/` 與 `runtime-manifest.json` 並用本機 `replace` 固定依賴。日後升級時請使用[安全升級工作流](../reference/upgrading.md)，切勿只手動替換部分檔案。

---

## 2. 透過 Docker Compose 一鍵啟動服務

在 **`bookstore`** 根目錄執行：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
docker compose up -d --build --wait
(Invoke-WebRequest http://localhost:18281/readyz).StatusCode
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
docker compose up -d --build --wait
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:18281/readyz
```

</TabItem>
</Tabs>

預期輸出 **`204`**。Compose 會自動依序啟動：PostgreSQL → 本機示範 IdP → 一次性部署 Migration 程序（獨立 migrator 角色）→ API（受 RLS 保護的 app 角色）。若尚未就緒，可執行 `docker compose logs api migrate` 查看遷移是否完成。

---

## 3. 實測登入、建立書籍與「冪等防重」

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 1. 取得 alice@tenant-a 的 JWT（有效 5 分鐘）
$login = Invoke-RestMethod -Method Post http://localhost:18280/token `
  -ContentType application/json -Body '{"subject":"alice","tenant_id":"tenant-a"}'

# 2. 帶上 Authorization 與 Idempotency-Key 建立書籍
$headers = @{
  Authorization     = "Bearer $($login.access_token)"
  'Idempotency-Key' = 'first-book'
}
$body = '{"id":"book-1","title":"My first generated app"}'
Invoke-RestMethod -Method Post http://localhost:18281/books `
  -Headers $headers -ContentType application/json -Body $body

# 3. 讀回剛建立的書籍
Invoke-RestMethod http://localhost:18281/books/book-1 -Headers $headers
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 1. 取得 alice@tenant-a 的 JWT（有效 5 分鐘）
TOKEN=$(curl -fsS -X POST http://localhost:18280/token \
  -H 'Content-Type: application/json' \
  -d '{"subject":"alice","tenant_id":"tenant-a"}' \
  | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')

# 2. 帶上 Authorization 與 Idempotency-Key 建立書籍
curl -fsS -X POST http://localhost:18281/books \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Idempotency-Key: first-book' \
  -H 'Content-Type: application/json' \
  -d '{"id":"book-1","title":"My first generated app"}'
echo

# 3. 讀回剛建立的書籍
curl -fsS http://localhost:18281/books/book-1 \
  -H "Authorization: Bearer $TOKEN"
echo
```

</TabItem>
</Tabs>

### 動手驗證冪等保護（試試重送！）
- **原封不動再跑一次同一個 `POST`**：因為 `Idempotency-Key` 與 `body` 完全相同，伺服器不會重複寫入，而是安全重播原本的結果（HTTP 200）。
- **沿用 `'first-book'` 但改掉 `title`**：預期直接回傳 **`409 Conflict`**（`IDEMPOTENCY_CONFLICT`），防堵誤用舊 key 送出新資料！

> **背後做了什麼**：API 已在單一交易內原子保存了書籍、必要 Audit、冪等成功結果與 Outbox 事件。若需要把 Outbox 配送到 NATS，可再依[可靠事件配送](../events/reliable-delivery.md)掛接獨立 Worker。

---

## 4. 需要多團隊組織隔離？生成 `--with organization` Profile

回到上層目錄另建一個啟用組織能力的專案：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
cd ..
& $pangolin new bookstore-org --template modular-monolith --with organization
cd bookstore-org
go test ./...
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
cd ..
"$PANGOLIN" new bookstore-org --template modular-monolith --with organization
cd bookstore-org
go test ./...
```

</TabItem>
</Tabs>

加上 `--with organization` 會一次生成完整 Enterprise／Organization 治理模組、管理 API、ScopeAware Migration 與對應的 Bookstore 實作（預設 `tenant-only` 完全不含組織碼或資料表）。

若前一個專案還在運行，改用另一組 port 啟動即可避免衝突：

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

向 `http://localhost:18290/token` 取得 `alice`/`tenant-a` Token 後，呼叫 `18291` 並帶入 Header：
- **Org A**：`X-Scope-Kind: org.organization`、`X-Scope-ID: 00000000-0000-4000-8000-000000000002`
- **Org B**：`X-Scope-ID` 結尾改為 `003`

兩個 Org 可以各自建立相同 `id` 的書籍且資料完全隔離。更多治理端點見[組織治理指南](../security/organizations.md)。

> **不可只改設定檔切換 Profile**：既有專案要從 `tenant-only` 升級為 `organization` 必須執行[前向資料庫遷移](../data/migrations.md)；API 啟動時會驗證資料庫 Profile，不符即拒絕 Ready。

---

## 5. 在自己專案新增第一個業務模組（實戰核心！）

在生成專案內，新增業務模組只需要四個指令：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 1. 新增具備完整契約、服務實作、建構子注入與單元測試的垂直切片模組
& $pangolin module add catalog --depends persistence,authorization --preset service

# 2. 產生全專案唯一序號的 Goose SQL 遷移骨架
& $pangolin migrate create add_catalog --module catalog

# 3. 輸出目前 Profile 的 OpenAPI 3.1 規格檔
& $pangolin generate openapi

# 4. 執行靜態專案與建置健康檢查
& $pangolin doctor

# 5. 立即執行測試驗證新模組可編譯且全部通過！
go test ./...
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 1. 新增具備完整契約、服務實作、建構子注入與單元測試的垂直切片模組
"$PANGOLIN" module add catalog --depends persistence,authorization --preset service

# 2. 產生全專案唯一序號的 Goose SQL 遷移骨架
"$PANGOLIN" migrate create add_catalog --module catalog

# 3. 輸出目前 Profile 的 OpenAPI 3.1 規格檔
"$PANGOLIN" generate openapi

# 4. 執行靜態專案與建置健康檢查
"$PANGOLIN" doctor

# 5. 立即執行測試驗證新模組可編譯且全部通過！
go test ./...
```

</TabItem>
</Tabs>

### 看懂生成專案的 Root 裝配結構（為什麼這麼好擴充？）
- **`foundation` 平台模組**：統一擁有 DB、UOW、Scope、Outbox、Idempotency 等基礎設施並 Export 公開契約。
- **`--depends persistence,authorization`**：CLI 自動把它翻譯成 `module.Of[foundation.Module]()` 的編譯期 Type 相依，你的服務建構子只需直接把 `uow.UnitOfWorkManager`、`foundation.ScopeAccess` 寫在參數上即可自動注入！
- **`--preset service`**：直接生成可通過 `go test ./...` 的 `application` 契約／實作、使用 `module.AddSingleton` 與 `module.Resolve[T]` 的模組接線、錯誤路徑與單元測試（省略 `--preset` 則只生精簡骨架；`--preset crud` 目前明示回報不支援，不產生半成品）。
- **`pangolin doctor` vs `pangolin doctor --live`**：預設 `doctor` 只做離線靜態檢查（檔案、manifest、`go build ./...`），DB／Migration／API 顯示 `SKIP`；環境啟動後再跑 **`pangolin doctor --live`** 即可逐層驗證 Compose、資料庫連線、Migration Ledger 與 `/livez`／`/readyz`（絕不回顯 DSN 機密）。

---

## 6. 停止並清理示範環境

在生成專案目錄內執行以下指令，即可安全清除該專案專屬容器與本機 volume：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
docker compose down --volumes --rmi local
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
docker compose down --volumes --rmi local
```

</TabItem>
</Tabs>

## 下一步

- 查閱完整 CLI 各子命令參數與環境變數清單：[pangolin CLI 參考](../reference/cli.md)（包含[建立專案](../reference/cli-new.md)、[新增模組](../reference/cli-module.md)、[遷移與健檢](../reference/cli-migrate-doctor.md)與[規格與升級](../reference/cli-openapi-upgrade.md)）。
- 為你的新模組加入多租戶動態偏好與快取：[分層設定與 Features](../platform/settings.mdx)。
- 了解日後如何安全升級生成專案的 Framework Runtime：[選擇版本與升級](../reference/upgrading.md)。
