---
title: 新增模組：pangolin module add
description: 使用 pangolin module add 新增預設骨架或 --preset service 垂直切片，理解 --depends 轉譯、UserRoot 聚合與建構子注入規則。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

`pangolin module add` 在已生成的 panGOlin 專案內新增一個業務模組，並以原子 staging 方式同步更新 `modules/registry.go`（`UserRoot` 聚合）與 `pangolin.json`。

> **工作目錄提醒**：請在**生成專案根目錄**執行本頁指令。

---

## 1. 語法與旗標說明

```text
pangolin module add NAME [--depends persistence,authorization] [--preset service]
```

| 參數／旗標 | 必填 | 預設值 | 說明 |
|---|---|---|---|
| `NAME` | 是（須在 flags 之前） | 無 | 模組套件名稱。須符合 `^[a-z][a-z0-9_]{0,47}$`，不可為 Go 保留字或 `main`，亦不可與既有模組重名 |
| `--depends` | 否 | 空 | 以逗號分隔的模組相依清單（例如 `persistence,authorization`、`organization`、`realtime` 或其他已註冊的使用者模組） |
| `--preset` | 否 | 空（精簡骨架） | 傳入 `service` 可生成完整可編譯且含單元測試的垂直切片；`crud` 目前明示拒絕 |

---

## 2. 預設骨架 vs `--preset service` 垂直切片

| 比較項目 | 省略 `--preset`（預設精簡骨架） | `--preset service`（推薦垂直切片） |
|---|---|---|
| **生成檔案** | `modules/<name>/module.go`<br/>`modules/<name>/README.md` | `modules/<name>/module.go`<br/>`modules/<name>/application/contracts.go`<br/>`modules/<name>/application/service.go`<br/>`modules/<name>/application/service_test.go`<br/>`modules/<name>/README.md` |
| **DI 與依賴宣告** | 產生 `Module`、`PreConfigure`、`Configure` 空架；若有 `--depends` 則加入對應 `DependsOn()` | 自動相依 `foundation.Module`，在 `Configure` 以 `module.AddSingleton(b.Services(), application.NewService)` 註冊，並在 `Start` 以 `module.Resolve[*application.Service]` 驗證接線 |
| **建構子注入與政策** | 留待自行實作 | 產生 `application.Contract` 公開介面與 `application.Service`，建構子型別化注入 `uow.UnitOfWorkManager` 與 `foundation.ScopeAccess`，並示範以 `frameworkapplication.QueryResult` 開啟唯讀政策及處理 `ErrUnavailable` |
| **開箱測試** | 專案可編譯 | 內附 `TestServiceStatus`（正常路徑、context 取消、未初始化依賴三條測試），`go test ./...` 立即全綠 |

### 為何刻意拒絕 `--preset crud`？

若傳入 `--preset crud`，CLI 會直接回報：

```text
unsupported preset "crud": only "service" is supported; no files changed
```

設計原因很明確：真實的多租戶與 ScopeAware 系統中，每個業務資料表都有各自的 owner 邊界、唯一鍵、RLS Policy、樂觀鎖、冪等策略與 Audit 要求。硬套通用 CRUD 程式碼產生器只會產出無法直接上線、甚至暗藏權限破口的半成品。`--preset service` 專注提供乾淨、符合 panGOlin 契約與測試規範的垂直切片起點，讓你安全地往下長出業務邏輯。

---

## 3. `--depends` 合法候選值與 Type 相依轉譯

`--depends` 只接受目前專案 Profile 中**合法且已存在**的候選依賴；CLI 會自動將它們轉譯為編譯期的 `module.Ref` Type 相依：

| `--depends` 候選名稱 | 可用條件 | 轉譯後的 Go `DependsOn()` Type 相依 |
|---|---|---|
| `persistence` | 全部 Profile | `module.Of[foundation.Module]()` |
| `authorization` | 全部 Profile | `module.Of[foundation.Module]()` |
| `organization` | 僅 `--with organization` 專案 | `module.Of[foundation.Module]()` |
| `realtime` | 僅 `--realtime` 專案 | `module.Of[realtime.Module]()`（可於建構子注入 `realtime.Publisher`） |
| 其他使用者模組（如 `catalog`） | 已透過 `module add` 註冊且不形成循環 | `module.Of[m_catalog.Module]()` |

### 為何拒絕 `--depends bookstore`？

`bookstore` 是生成專案內建的示範業務模組，不是平台基礎層。如果新模組依賴 `bookstore`，未來移除或替換示範程式碼時會導致整個應用耦合斷裂。因此 CLI 主動攔截 `--depends bookstore` 並列出當前 Profile 所有合法候選名稱。

---

## 4. 生成專案的 Root `UserRoot` 聚合與建構子注入

生成專案的啟動接線分為三層：

```text
internal/app（rootApplication 應用根模組）  ◀── 負責 HTTP 路由、Drain Participant 與啟動相容檢查
   ├─ foundation.Module / foundation.Profile ◀── 平台層：擁有 DB/UOW/Scope/Outbox 並 Export 契約
   └─ modules/registry.go (UserRoot)         ◀── 聚合所有你用 pangolin module add 新增的業務模組
```

每次成功執行 `pangolin module add`，CLI 都會重新生成 `modules/registry.go`，將你的新模組型別加入 `UserRoot.DependsOn()`。因此你不需要手動去改 `internal/app` 就能讓模組進入啟動規劃圖。

在你的業務模組中（以 `--preset service` 產出的 `modules/catalog/application/service.go` 為例），只需透過**標準 Go 建構子**宣告需要注入的平台契約：

```go
type Service struct {
	units  uow.UnitOfWorkManager
	scopes foundation.ScopeAccess
}

func NewService(units uow.UnitOfWorkManager, scopes foundation.ScopeAccess) *Service {
	return &Service{units: units, scopes: scopes}
}
```

並在 `modules/catalog/module.go` 的 `Configure` 註冊：

```go
func (m *Module) Configure(b module.RootBuilder) error {
	return module.AddSingleton(b.Services(), application.NewService)
}
```

> **重要提醒**：`modules/registry.go` 由 CLI 自動維護。如果你手動修改了 `modules/registry.go`，之後再執行 `pangolin module add` 時 CLI 會保護你的修改並拒絕覆寫（`module registry was edited; existing files preserved`）。

---

## 5. 雙環境操作範例與預期輸出

在生成專案根目錄執行：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
# 1. 新增帶完整垂直切片與測試的 catalog 模組
pangolin module add catalog --depends persistence,authorization --preset service

# 2. 新增依賴 catalog 的 pricing 精簡骨架模組
pangolin module add pricing --depends catalog

# 3. 驗證編譯與單元測試全部通過
go test ./...
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
# 1. 新增帶完整垂直切片與測試的 catalog 模組
pangolin module add catalog --depends persistence,authorization --preset service

# 2. 新增依賴 catalog 的 pricing 精簡骨架模組
pangolin module add pricing --depends catalog

# 3. 驗證編譯與單元測試全部通過
go test ./...
```

</TabItem>
</Tabs>

### 預期輸出

```text
Generated catalog preset service
Generated pricing preset skeleton
```

接著執行 `go test ./...` 應全部 PASS。

---

## 6. 常見錯誤與安全防護

| 錯誤訊息 | 原因 | 處置方式 |
|---|---|---|
| `module already registered` / `module directory already exists` | `pangolin.json` 已登記該模組或 `modules/<name>` 已存在 | 改用新模組名稱；CLI 絕不覆寫既有模組 |
| `dependency path <name> -> bookstore: ...` | 嘗試相依示範模組 `bookstore` | 改為相依 `persistence`、`authorization`、`organization`、`realtime` 或其他自建業務模組 |
| `dependency path <name> -> <dep>: missing module; valid candidates: ...` | 指定的相依模組尚未註冊，或目前 Profile 未啟用該能力（例如 `tenant-only` 下寫 `organization`） | 依錯誤訊息列出的 `valid candidates` 修正 `--depends` |
| `module registry was edited; existing files preserved` | `modules/registry.go` 與 `pangolin.json` 推算出的內容不一致（被手動改動） | 還原 `modules/registry.go` 為 CLI 產生格式，或選擇自行手工維護模組接線 |
| `module generation already in progress; project files preserved` | 專案根目錄存在 `.pangolin-module.lock` 併發鎖 | 等待另一個 CLI 指令完成；若確認前次程序異常中止，手動檢查後刪除殘留鎖檔 |
| `unsupported preset "crud": only "service" is supported; no files changed` | 傳入了不支援的 preset 值 | 改用 `--preset service` 或省略 `--preset` |

## 下一步

為新建立的業務模組產生資料庫遷移並執行健康檢查：前往 **[遷移與健檢：`pangolin migrate` 與 `pangolin doctor`](cli-migrate-doctor.md)**。
