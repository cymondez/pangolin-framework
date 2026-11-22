# 目錄整理回歸紀錄

日期：2026-11-21。來源 commit `b3bf59c03f4b66ff7389d44d8434356258a85d9a`，分支 `codex/m1-vertical-flow`。本次為 M1 之後的程式歸屬整理，沒有新增產品需求或提前交付 M2；原 [M1 驗收紀錄](M1.md) 與來源 `9eb10df6` 的 evidence 保持不變。後續狀態文件 commit 不取代本次測試來源。

## 變更與相容性

Bookstore 的 API／Worker main、專用 HTTP Handler、crash-worker／m1-query 已全部歸 `examples/bookstore`。共用 fixture 留在根目錄 internal/testkit；單元測試並置，跨套件測試、工具與操作腳本各有索引。根目錄不增加 src，也不保留空 cmd／transport 或會反向依賴範例的轉接 package。

以 M1 的 API snapshot 比對本次 snapshot：只有 Bookstore 的兩個 main package 與 HTTP package 改路徑，其餘 package 的公開宣告完全相同；Handler 也只有 canonical import path 變更。實作／測試檔案搬移保持內容，bootstrap 只更換 Handler import。新命令與 import 對照見 [搬移說明](../release-notes/source-layout.md)，歸屬原則見 [ADR10](../adr/ADR10-source-ownership.md)。

## 同版回歸

`python scripts/m1.py gate --commit b3bf59c03f4b66ff7389d44d8434356258a85d9a` 通過，Windows／固定 OCI Linux runner 的 report 都來自乾淨的同一 commit／source hash。

- 完整 build／vet／依賴與 license、M0／M1 工程反例、47 個既有具名子情境對應檢查通過。
- 新增並先驗證失敗的 framework → examples／Application → 具體 Transport 反例，再完成搬移與規則；實際 go-list graph 注入與現行正向依賴圖通過驗證。
- 真實 PostgreSQL／NATS Integration 與分程序 E2E 從新位置建置／執行；提交、回滾、JWT／RLS、Broker ACK 後崩潰及同 EventID 重送行為不變。
- Linux 包含真實 Integration 的 race 通過；sqlc v1.31.1 再生成比對通過。
- 25 份模組／工具／測試入口文件的存在與本機連結檢查通過；現行 Markdown 連結另已掃描，沒有失效連結。

| Runner | 整體手寫 production statements | 各關鍵 package |
|---|---:|---|
| Windows/amd64 | 1341／1430（93.7762%） | 全部依原始計數通過 90% |
| Linux/amd64 | 1340／1430（93.7063%） | 全部依原始計數通過 90% |

Unit／Integration／插樁 E2E 在各自相同 build 下合併；沒有將搬移前後或不同平台的 profile 混合。

## 證據與環境

[機器可讀索引](source-layout-b3bf59c0.json) 保存 Gate／report hash、API 搬移對照、coverage 計數與環境 hash。完整 raw profiles、HTML／JSON、Go manifest、log 與插樁資料位於 `artifacts/b3bf59c03f4b66ff7389d44d8434356258a85d9a/m1/`。

Windows report SHA-256：`9a52a69c9a35c88425f366ceebb8301b01eccddb8010c8e0d79517ed9cb01e0c`。Linux report SHA-256：`85d899d1315a51f52e356732ae90171a033c55933e3e1c3f8893ba3bc4786a15`。

驗證環境為新 namespace `pangolin-m1-layout-b3bf59c0` 與空 volumes，固定服務 image 及來源 label 已保存。三個測試容器與 runner 均已停止／移除，自己的 volumes 保留；其他專案持續運作，沒有重啟 Engine、WSL 或全域 prune。此為本機 Windows／Linux 證據，尚未推送或執行遠端 CI。
