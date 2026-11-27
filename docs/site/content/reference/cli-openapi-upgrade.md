---
title: 規格與升級：pangolin generate openapi 與 pangolin upgrade
description: 使用 pangolin generate openapi 輸出 OpenAPI 3.1 規格，並以 upgrade plan / --dry-run / runtime / verify 安全升級生成專案內附的 framework/ Runtime。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

這一頁涵蓋兩項專案維護指令：
1. **`pangolin generate openapi`**：輸出目前 Profile 的 OpenAPI 3.1 JSON 規格並保護既有修改。
2. **`pangolin upgrade`**：透過四步驟三路比對安全升級生成專案內附的 `framework/` Runtime。

> **分工說明**：本頁專注於 CLI 指令參數、執行輸出、備份與回滾操作；完整的版本治理原則、依賴固定策略與七種三路比對分類定義，請對照閱讀 **[選擇版本與安全升級指南](upgrading.md)**。

---

## 1. 輸出 OpenAPI 3.1 規格：`pangolin generate openapi`

```text
pangolin generate openapi
```

### 行為與重入保護

- 在生成專案根目錄讀取 `pangolin.json` 的 `profile`（`tenant-only` 或 `organization-enabled`），輸出對應的 OpenAPI 3.1 JSON 檔案至專案根目錄 **`openapi.json`**。
  - `tenant-only` 包含基礎 Bookstore API（`POST /books`、`GET /books/{id}`、健康檢查）。
  - `organization-enabled` 額外包含完整 Enterprise／Organization 治理端點與 Scope 標頭定義。
- **冪等重入**：若 `openapi.json` 已存在且內容與範本完全一致，命令會安全成功（exit code 0）且不改動檔案。
- **不覆寫使用者修改**：若 `openapi.json` 已存在且你手動修改過內容，CLI 會保留原檔並直接回傳錯誤：
  ```text
  openapi.json differs; existing file preserved
  ```

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 在生成專案根目錄產生 openapi.json
& $pangolin generate openapi
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 在生成專案根目錄產生 openapi.json
"$PANGOLIN" generate openapi
```

</TabItem>
</Tabs>

---

## 2. 安全升級內附 Runtime：`pangolin upgrade`

使用 `pangolin new` 建立的專案內附 `framework/` 與 `runtime-manifest.json`。當你取得新版 `pangolin` CLI 後，一律在**生成專案根目錄**依序執行以下四個子命令：

| 步驟 | 命令 | 寫入磁碟？ | 作用與阻擋條件 |
|---|---|---|---|
| **1. 預覽差異** | `pangolin upgrade plan` | 否（純唯讀） | 印出目前版本、目標版本、建置 revision、每個受管檔案的三路比對分類，以及 `api inventory`／`migrations`／`settings/config` 變更統計。若有 `conflict` 或 `missing` 回非零結束碼。 |
| **2. 乾跑檢查** | `pangolin upgrade --dry-run` | 否（純唯讀） | 同 `plan` 執行完整三路比對與基線能力核對，不改動任何檔案；適合放進 CI 或部署前驗證腳本。 |
| **3. 套用升級** | `pangolin upgrade runtime` | 是（先備份再原子替換） | 取得專案併發鎖、重新驗證零衝突、先完整備份至 `.pangolin-backup/<UTC>/`，於暫存區組裝並驗證全部 SHA-256 後原子切換 `framework/`、`runtime-manifest.json` 與 `pangolin.json`。 |
| **4. 完整性驗證** | `pangolin upgrade verify` | 否（純唯讀） | 核對 `pangolin.json` 版本、`runtime-manifest.json` 與全部本地 `framework/` SHA-256，並依序以 `-mod=readonly` 執行 `go build ./...`、`go test ./...` 與 `go vet ./...`。 |

### 雙環境四步升級操作範例

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 步驟 1：唯讀預覽版本與檔案差異
& $pangolin upgrade plan

# 步驟 2：唯讀乾跑確認無衝突或漏檔
& $pangolin upgrade --dry-run

# 步驟 3：備份並套用新版 framework/ Runtime
& $pangolin upgrade runtime

# 步驟 4：驗證全部 checksum 並以 -mod=readonly 執行 build / test / vet
& $pangolin upgrade verify
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 步驟 1：唯讀預覽版本與檔案差異
"$PANGOLIN" upgrade plan

# 步驟 2：唯讀乾跑確認無衝突或漏檔
"$PANGOLIN" upgrade --dry-run

# 步驟 3：備份並套用新版 framework/ Runtime
"$PANGOLIN" upgrade runtime

# 步驟 4：驗證全部 checksum 並以 -mod=readonly 執行 build / test / vet
"$PANGOLIN" upgrade verify
```

</TabItem>
</Tabs>

### 預期輸出摘要

- **`upgrade plan` / `--dry-run`**：
  ```text
  current version: 0.1.0
  target version: 0.1.0
  framework/... unchanged
  ...
  api inventory: 0 public Go file(s) changed
  migrations: 0 file(s) changed
  settings/config: 0 file(s) changed
  ```
- **`upgrade runtime`**（結尾會印出實際建立的備份路徑與回滾提示）：
  ```text
  upgraded runtime to 0.1.0
  backup: .pangolin-backup/20261127T080000Z
  rollback: ...
  ```
- **`upgrade verify`**：
  ```text
  [PASS] project files
  [PASS] runtime manifest
  [PASS] go build ./...
  [PASS] go test ./...
  [PASS] go vet ./...
  [PASS] upgrade verify
  ```

---

## 3. 自動回復與手動回滾

- **套用中途自動回復**：如果在 `upgrade runtime` 切換或落地後雜湊驗證失敗（或收到 `Ctrl+C` 中斷），CLI 會自動從 `.pangolin-backup/<UTC>/` 與暫存區嘗試還原 `framework/`、`runtime-manifest.json` 與 `pangolin.json`。
- **升級後手動回滾**：若升級後發現業務層不符預期，將以下 `<UTC>` 替換為 `upgrade runtime` 印出的備份目錄名稱即可完整還原：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
$backup = '.pangolin-backup/<UTC>'
Remove-Item -Recurse -Force .\framework
Copy-Item -Recurse "$backup\framework" .\framework
Copy-Item "$backup\runtime-manifest.json" .\runtime-manifest.json
Copy-Item "$backup\pangolin.json" .\pangolin.json
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
BACKUP='.pangolin-backup/<UTC>'
rm -rf ./framework
cp -R "$BACKUP/framework" ./framework
cp "$BACKUP/runtime-manifest.json" ./runtime-manifest.json
cp "$BACKUP/pangolin.json" ./pangolin.json
```

</TabItem>
</Tabs>

若本次升級包含新的 SQL Migration，請先於驗證通過後執行 `go run ./cmd/migrate`，再重新啟動 API（若需還原資料庫請依[備份、還原與升級檢查](../platform/backup-restore.md)處理，不使用 Down Migration）。

---

## 4. 常見阻擋與錯誤訊息

| 錯誤訊息 | 原因 | 處置方式 |
|---|---|---|
| `upgrade plan blocked: conflict or missing files` / `upgrade runtime refused: conflict or missing files` | 本地手動修改或刪除了 `framework/` 內的受管檔案 | 檢查印出的 `conflict`（含 `base`/`local`/`target` SHA-256）或 `missing` 路徑，先還原或解決差異後再重跑 `upgrade plan` |
| `upgrade refused: profile switch is not an in-place upgrade` | 修改了 `pangolin.json` 的 `profile`，與 `runtime-manifest.json` 基線不符 | 切換 `tenant-only` 與 `organization-enabled` 不可靠原地升級，請依[部署資料庫遷移](../data/migrations.md)進行前向遷移 |
| `upgrade refused: realtime capability switch is not an in-place upgrade` | 修改了 `pangolin.json` 的 `realtime` 旗標，與基線不符 | 不可靠單純改寫設定切換即時能力，請依[選擇版本與安全升級指南](upgrading.md)另行規劃 |
| `upgrade runtime refused: project changed while planning; rerun upgrade plan` | 規劃與套用期間有其他程序更動了專案檔案 | 確認無其他編輯或腳本正在修改檔案後，重新執行 `upgrade plan` |
| `upgrade verify: stale pangolin.json version ...` | 尚未執行 `upgrade runtime` 或版本不一致 | 確認已完成 `upgrade runtime` 且使用同源 CLI 執行 `verify` |
