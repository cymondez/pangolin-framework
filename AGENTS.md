# 開發入口

先閱讀 [AGENT.md](AGENT.md)，再依需求閱讀實作規範及里程碑。Windows shell 呼叫使用 `login=false`；工具 discovery／版本與本機路徑依 AGENT.md。

## 共用開發環境

- 操作 Docker／Compose 前先唯讀盤點現有容器、Compose 專案、連接埠、volume 與資源占用，確認本次操作的影響範圍。
- 本專案環境使用明確的 Compose project 名稱、自己的 volume／network、未占用的 loopback port 與資源上限；不採用或修改其他專案的容器。啟停前確認目標確屬本次工作。
- Docker Desktop／Engine、WSL 或共用系統服務的啟停、重設，以及全域 prune／清理等跨專案操作，需要使用者針對該操作明確同意。連線失敗先區分 engine 狀態、存取權限、context 與工具問題，不自行重啟來排錯。
- 上述規則要求先確認環境及最小影響；已獲授權且確認隔離的專案 Docker 操作應正常進行，不擴大解讀成全面禁用 Docker。
