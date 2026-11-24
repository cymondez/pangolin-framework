---
title: 安裝與建立專案
description: 準備 Go，並將 panGOlin 預覽套件加入自己的應用專案。
---

完成這一頁後，你會有一個獨立的 Go 專案，可以 import panGOlin 的公開套件。

## 需要準備什麼？

- **Go 1.27.4**：執行 `go version` 確認版本。
- **Git**：用來取得目前的預覽套件。
- 任意文字編輯器，例如 VS Code。

第一個程式不需要 Docker、資料庫、Node.js 或 Python。這些工具只在你選擇需要它們的教學時才安裝。

## 1. 準備工作資料夾

0.1.0 預覽套件目前使用本機來源安裝。選擇自己的工作位置，執行：

```bash
mkdir pangolin-work
cd pangolin-work
git clone https://github.com/cymondez/pangolin-framework.git
mkdir hello-pangolin
cd hello-pangolin
```

如果已經有包含本文件範例的 Framework checkout，可以直接使用它，並在相鄰位置建立 `hello-pangolin`。以下目錄應該並排：

```text
pangolin-work/
  pangolin-framework/   套件來源
  hello-pangolin/       你自己的應用
```

請確認 `pangolin-framework/examples/basics/hello/main.go` 存在；若沒有，請使用提供這份預覽文件的原始碼版本。預覽版尚未提供可直接下載的正式 `v0.1.0` tag。

## 2. 把套件加入你的專案

**在 `hello-pangolin` 資料夾執行：**

```bash
go mod init example.com/hello-pangolin
go mod edit -require=github.com/cymondez/pangolin-framework@v0.1.0
go mod edit -replace=github.com/cymondez/pangolin-framework=../pangolin-framework
```

`require` 宣告使用的套件；`replace` 告訴 Go 從相鄰的本機資料夾讀取它。此處的版本宣告搭配本機 replace 使用，不會嘗試下載尚未發布的 tag。

這時還沒有 `.go` 檔案，因此先不要執行 `go mod tidy`；下一頁加入程式後再整理依賴。

## 3. 繼續建立第一個程式

保持終端機位於 `hello-pangolin`，前往[第一個應用](quickstart.mdx)。

若你只想先執行現成範例，也可以從套件 checkout 的 `examples/basics` 執行 `go run ./hello`；[範例索引](../samples/index.md)列出每個範例的用途。

## 4. 或用 CLI 生成完整專案

不想手動組裝時，可從 Framework checkout 建置 `pangolin` CLI，一次生成有登入、資料庫與租戶隔離的獨立 Bookstore 專案；見[用 CLI 建立自己的 Bookstore](../tutorials/generated-bookstore.md)。命令、選項與生成專案的環境變數見 [pangolin CLI](../reference/cli.md)。
