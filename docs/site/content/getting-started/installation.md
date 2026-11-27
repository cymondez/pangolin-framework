---
title: 安裝與建立專案
description: 準備 Go 環境，並建立一個獨立的 consumer 專案來引用 panGOlin 0.1.0 預覽套件。
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

完成這一頁後，你會在自己的工作目錄下建立一個全新的 Go 專案 **`hello-pangolin`**，並正確連結 panGOlin 的公開套件。

> **重要觀念：你是在「建立自己的應用」，而不是在「修改框架原始碼」**
> 接下來我們會把 `pangolin-framework`（框架來源）與 `hello-pangolin`（你的應用）放在並排的兩個資料夾，模擬真實專案把 panGOlin 當成第三方套件 import 的方式。

---

## 1. 環境與工具準備

### 基礎起步（本頁與前幾篇教學）只需要：
- **Go 1.27.4**（或以上相容版本）：在終端機執行 `go version` 確認。
- **Git**：用來下載 panGOlin 預覽版原始碼。
- 任意 Go 編輯器（如 VS Code + Go 擴充套件，或 GoLand）。

### 暫時**不需要**安裝：
- Docker、PostgreSQL、NATS、Node.js 或 Python —— 這些工具等到你進入 [Bookstore 實戰教學](../tutorials/bookstore.md) 或 [交易傳播教學](../tutorials/transactions.mdx) 時才需要。

---

## 2. 準備並排的工作資料夾

因為目前是 **0.1.0 預覽版**，尚未對外發布遠端 `v0.1.0` Git tag，所以先透過本機路徑引入套件。請選擇你的工作位置執行：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
mkdir pangolin-work
cd pangolin-work
git clone https://github.com/cymondez/pangolin-framework.git
mkdir hello-pangolin
cd hello-pangolin
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
mkdir -p pangolin-work
cd pangolin-work
git clone https://github.com/cymondez/pangolin-framework.git
mkdir -p hello-pangolin
cd hello-pangolin
```

</TabItem>
</Tabs>

如果你手邊已經有一份 `pangolin-framework` 原始碼，不需要重新 `git clone`，只要在它旁邊建立 `hello-pangolin` 資料夾即可。完成後的目錄應如下所示：

```text
pangolin-work/
  pangolin-framework/   ← 框架套件來源（唯讀參考，不用改這裡的檔案）
  hello-pangolin/       ← 你自己的應用專案（接下來都在這裡寫程式）
```

> **快速檢查**：確認 `pangolin-framework/examples/basics/hello/main.go` 檔案存在，代表你取得的版本包含本手冊對應的預覽 API。

---

## 3. 在你的專案中引入 panGOlin

確認終端機目前位於 **`hello-pangolin`** 資料夾內，依序執行：

<Tabs groupId="os">
<TabItem value="powershell" label="Windows PowerShell">

```powershell
go mod init example.com/hello-pangolin
go mod edit -require=github.com/cymondez/pangolin-framework@v0.1.0
go mod edit -replace=github.com/cymondez/pangolin-framework=../pangolin-framework
```

</TabItem>
<TabItem value="bash" label="Linux / macOS (Bash)">

```bash
go mod init example.com/hello-pangolin
go mod edit -require=github.com/cymondez/pangolin-framework@v0.1.0
go mod edit -replace=github.com/cymondez/pangolin-framework=../pangolin-framework
```

</TabItem>
</Tabs>

### 為什麼要寫 `require` 又寫 `replace`？

- **`-require=...v0.1.0`**：在 `go.mod` 中正式宣告專案相依於 panGOlin 套件。
- **`-replace=...=../pangolin-framework`**：告訴 Go 工具鏈「不要去網路上找尚未發布的 `v0.1.0` tag，直接讀取旁邊本機資料夾的程式碼」。未來正式版發布後，只需移除這行 `replace` 即可無痛切回遠端下載。

> **此時先不要執行 `go mod tidy`**：因為 `hello-pangolin` 裡還沒有任何 `.go` 檔案，現在執行 `go mod tidy` 會把尚未被 import 的 `require` 自動清掉。下一頁寫完 `main.go` 後我們再執行 `go mod tidy`。

---

## 4. 選擇你的下一步

依據你的目標選擇合適的起點：

- **從零手寫第一個應用（最推薦給新手）**：保持終端機在 `hello-pangolin`，直接前往 **[第一個應用](quickstart.mdx)**，3 分鐘跑出 `Hello, panGOlin!`。
- **想直接生成完整專案骨架**：如果你想跳過手動組裝，直接用框架 CLI 生成一個具備登入、PostgreSQL 與多租戶隔離的獨立專案，請前往 **[用 CLI 建立自己的 Bookstore](../tutorials/generated-bookstore.md)**（完整指令見 [pangolin CLI 參考](../reference/cli.md)）。
- **只想先瀏覽現成範例**：直接切到 `pangolin-framework/examples/basics` 執行 `go run ./hello`，所有免資料庫範例都整理在 **[套件使用範例總覽](../samples/index.md)**。
