# ADR10 — 程式碼先按擁有者歸類

日期：2026-11-21。狀態：採用；依使用者確認的目錄整理方向。承接 ADR01、實作規範 §1.1／§10，落實 ARC-001；不改 M1 的交易、安全或事件保證。

## 決策

保留 repository root 的單一 go.mod 與 framework 公開套件路徑，不加 src。kernel、persistence、security、events 等共用套件只依賴自己的抽象／Adapter；production graph 不得 import examples。

Bookstore 自己擁有 API／Worker main、HTTP Handler、bootstrap、Domain／Application／Infrastructure，以及只能用於 Bookstore 的 crash-worker／m1-query。這些檔案集中於 examples/bookstore；共用測試 IdP 與 Profile assemblies 留在根目錄 internal/testkit。

測試依範圍歸類：同套件 `_test.go` 並置；跨套件／服務驗收與規範資料歸 tests；受 internal 可見性保護的共用 fixture 歸 internal/testkit；操作腳本歸 scripts，Go 檢查命令歸 tools。兩個目錄各有 README，從根目錄工具／測試索引進入；不再以檔名猜用途。

## 相容性與驗證

framework 既有公開 Port／import path 維持原樣。Bookstore Handler 的 import path 及 go run／build 入口需要遷移；對照表見 [搬移說明](../release-notes/source-layout.md)。不留會繼續讓框架依賴範例的轉接 package。

更新 API snapshot、case／coverage manifest、E2E 建置路徑與所有現行文件；保留舊 commit 的 evidence 與 hash。Gate 驗證實際 go-list graph，拒絕 framework → examples 及 Application → 具體 Transport。以完整 M1 Unit／Integration／分程序 E2E／coverage／Linux race 回歸確認搬移後的行為。

## 取捨

根目錄仍有多個公開能力 package，能維持清楚的 Go import 路徑；每個完整範例可從自己的目錄理解和啟動。暫不把 M1 範例中的 Replicator 提升為通用事件 Store，完整抽象仍按 M3 交付。
