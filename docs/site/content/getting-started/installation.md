---
title: 安裝與建立專案
description: 準備 Go 環境，建立獨立的應用專案並透過標準 Go module 安裝 panGOlin v0.1.0 套件與 CLI。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

完成這一頁後，你會在自己的工作目錄下建立一個全新的 Go 專案 **`hello-pangolin`**，並安裝 `panGOlin v0.1.0` 公開套件（需要專案腳手架時，也可一併安裝 `pangolin` CLI）。

---

## 1. 環境與工具準備

### 基礎起步（本頁與前幾篇教學）只需要：
- **Go 1.27.4**（或以上相容版本）：在終端機執行 `go version` 確認。
- 任意 Go 編輯器（如 VS Code + Go 擴充套件，或 GoLand）。

### 暫時**不需要**安裝：
- Docker、PostgreSQL、NATS、Node.js 或 Python —— 這些工具等到你進入 [Bookstore 實戰教學](../tutorials/bookstore.md) 或 [交易傳播教學](../tutorials/transactions.mdx) 時才需要。
- 若稍後想執行倉庫內建的完整範例程式（`examples/basics`、`examples/moduleapp`、`examples/bookstore`），才需要 **Git** 下載範例倉庫。

---

## 2. 建立你的應用專案並安裝 panGOlin

在任意工作目錄建立自己的專案 **`hello-pangolin`**，並透過標準 `go get` 引入 `v0.1.0`：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
mkdir hello-pangolin
cd hello-pangolin
go mod init example.com/hello-pangolin
go get github.com/pangolin-framework/pangolin@v0.1.0
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
mkdir -p hello-pangolin
cd hello-pangolin
go mod init example.com/hello-pangolin
go get github.com/pangolin-framework/pangolin@v0.1.0
```

</TabItem>
</Tabs>

完成後 `go.mod` 會鎖定 `github.com/pangolin-framework/pangolin v0.1.0`。

> **此時先不要執行 `go mod tidy`**：因為 `hello-pangolin` 裡還沒有任何 `.go` 檔案，現在執行 `go mod tidy` 會把尚未被 import 的 `require` 自動清掉。下一頁寫完 `main.go` 後再執行 `go mod tidy` 即可。

---

## 3. 安裝 `pangolin` CLI（可選）

如果你想用框架 CLI 一鍵建立完整的模組化單體專案骨架、新增業務模組或執行專案健檢，可以直接透過 `go install` 安裝同版 CLI：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
go install github.com/pangolin-framework/pangolin/cmd/pangolin@v0.1.0
pangolin --version
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
go install github.com/pangolin-framework/pangolin/cmd/pangolin@v0.1.0
pangolin --version
```

</TabItem>
</Tabs>

預期輸出 `0.1.0`（請確認 `go env GOPATH` 底下的 `bin` 目錄已加入系統 `PATH`）。完整指令與選項請見 [pangolin CLI 參考](../reference/cli.md)。

---

## 4. 選擇你的下一步

依據你的目標選擇合適的起點：

- **從零手寫第一個應用（最推薦給新手）**：保持終端機在 `hello-pangolin`，直接前往 **[第一個應用](quickstart.mdx)**，3 分鐘跑出 `Hello, panGOlin!`。
- **想直接生成完整專案骨架**：如果你想跳過手動組裝，直接用框架 CLI 生成一個具備登入、PostgreSQL 與多租戶隔離的獨立專案，請前往 **[用 CLI 建立自己的 Bookstore](../tutorials/generated-bookstore.md)**（完整指令見 [pangolin CLI 參考](../reference/cli.md)）。
- **想直接執行倉庫內的現成範例**：clone `v0.1.0` 範例倉庫後切到 `pangolin/examples/basics` 執行 `go run ./hello`，所有免資料庫範例都整理在 **[套件使用範例總覽](../samples/index.md)**。
