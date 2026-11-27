---
title: 選擇版本與安全升級指南
description: 說明如何固定 panGOlin 版本依賴、升級前的標準檢查，以及透過 pangolin upgrade 安全升級生成專案。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

> **你在架構中的位置**：`版本治理與維護` **`[ 鎖定正式版本 Tag → 預覽升級影響（upgrade plan）→ 備份遷移 → 驗證與回滾預案 ]`**

本文件對應 **panGOlin v0.1.0**。頁首版本選單用於切換對應版本的手冊；未來發布新版本後，歷史版本文件與程式碼範例將會凍結保留。

---

## 1. 如何穩定固定依賴？

- **標準 Go 專案**：透過 `go get github.com/pangolin-framework/pangolin@v0.1.0` 在 `go.mod` 固定具體發行版本，並將 `go.mod` 與 `go.sum` 一同納入版本控制，確保團隊與 CI 使用完全一致的相依版本。
- **CLI 生成專案**：確保安裝的 `pangolin` CLI（`go install github.com/pangolin-framework/pangolin/cmd/pangolin@v0.1.0`）、內附 `framework/` 與 `runtime-manifest.json` 維持同版一致；升級時一律透過目標版本的 `pangolin upgrade` 完成三路比對與雜湊驗證。

---

## 2. 升級任何應用前的 4 項標準檢查

1. 核對目標版本文件與 API／必要設定差異（並確認**事件 `SchemaVersion` 與框架版本獨立**——升級框架絕不可原地改寫既有事件格式）。
2. 先在隔離測試環境更新依賴並執行完整測試套件。
3. 確認資料庫 Schema 相容性；若有結構變更，務必先依[備份、還原與升級檢查](../platform/backup-restore.md)建立並驗證備份。
4. 確認回滾預案後，先跑部署遷移，再滾動更新 API 與 Worker。

---

## 3. CLI 生成專案的安全升級工作流（`pangolin upgrade`）

對於以 `pangolin new` 建立的專案，框架提供「**先預覽、零靜默覆寫、自動備份、可驗證回滾**」的四步升級流程（使用目標版本的同源 CLI 於生成專案根目錄執行；完整參數、預期輸出與回滾命令見[規格與升級 CLI 手冊](cli-openapi-upgrade.md)）：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 步驟 1：只讀預覽版本差異與逐檔變更分類（零寫入）
pangolin upgrade plan

# 步驟 2：只讀乾跑檢查衝突與缺檔；有衝突回非零結束碼（零寫入）
pangolin upgrade --dry-run

# 步驟 3：無衝突時安全套用新版 framework/ 與 manifest（自動備份至 .pangolin-backup/<UTC>/）
pangolin upgrade runtime

# 步驟 4：只讀驗證版本、全部 checksum，並以 -mod=readonly 執行 build / test / vet
pangolin upgrade verify
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 步驟 1：只讀預覽版本差異與逐檔變更分類（零寫入）
pangolin upgrade plan

# 步驟 2：只讀乾跑檢查衝突與缺檔；有衝突回非零結束碼（零寫入）
pangolin upgrade --dry-run

# 步驟 3：無衝突時安全套用新版 framework/ 與 manifest（自動備份至 .pangolin-backup/<UTC>/）
pangolin upgrade runtime

# 步驟 4：只讀驗證版本、全部 checksum，並以 -mod=readonly 執行 build / test / vet
pangolin upgrade verify
```

</TabItem>
</Tabs>

### 看懂 `plan` 的 7 種三路比對分類

| 分類 | 代表狀態（Base / Local / Target） | `upgrade runtime` 行為 |
|---|---|---|
| `unchanged` | 三者完全一致 | 保持不動 |
| `update` | 你沒改過此檔，新版有更新 | 安全更新至目標版 |
| `keep` | 你本地內容已與新版一致 | 保留本地檔案 |
| `add` | 新版新增的受管檔案 | 自動補入 |
| `remove` | 基準版有、你沒改過、目標版已移除 | 安全移除 |
| `conflict` | 你與目標版都改過此檔，或你刪除了有差異的受管檔 | **拒絕套用**，僅列出路徑與 SHA-256（不洩漏檔案內容），絕不靜默覆寫 |
| `missing` | 基準版與目標版相同，但你本地刪除了此受管檔 | **拒絕套用**，提示補回或處理 |

- 若 `runtime` 帶來新的 SQL Migration，升級並驗證完成後請執行 `go run ./cmd/migrate`，再啟動 API。
- 若套用中途出錯，CLI 會自動嘗試從 `.pangolin-backup/<UTC>/` 還原並附上明確的回滾指令（Windows PowerShell 與 Bash 手動還原步驟見[規格與升級 CLI 手冊](cli-openapi-upgrade.md#3-自動回復與手動回滾)）。
- **仍須人工審查的項目**：`plan` 摘要中的 API／Migration／設定數量變化僅供分類參考，DB Migration、事件 Schema 相容性、Profile 切換與第三方相容性仍須工程師人工 Review。

### 組織 Profile 不可靠改設定檔切換
`tenant-only` 與 `organization` 擁有不同的依賴圖與資料庫 Schema，不能只改 `pangolin.json` 切換。既有專案啟用組織須執行[前向遷移與 Inventory 檢查](../data/migrations.md)；API 啟動若發現資料庫 Profile 不符會直接拒絕 Ready。
