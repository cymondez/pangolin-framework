# 套件使用範例

這是一個獨立 Go module，透過 require／本機 replace 引用 Framework，展示應用開發者如何使用公開 API。下表的核心範例不需要 DB、Broker、Python 或 Node。

從本目錄執行：

| 命令 | 學習目標 |
|---|---|
| `go run ./hello` | 建立並執行第一個模組 |
| `go run ./greeting-app` | 組合 Module、DI 與應用設定 |
| `go run ./configuration` | 預設設定與環境覆寫 |
| `go run ./dependency-injection` | 以介面注入服務與關閉 Container |
| `go run ./lifecycle` | 模組相依、啟動與反序停機 |
| `go run ./event-envelope` | 建立、編碼與讀回 Integration Event |

完整教學位於 [官方文件來源](../../docs/site/content/intro.md)，網站建置時從這些原始碼生成展示片段，不手動維護另一份程式副本；正式文件快照保存對應的程式碼。各範例的 README 說明執行、設定與預期輸出。`python scripts/docs_examples.py` 從 repository root 在獨立使用者專案執行所有範例，驗證文件中的完整操作。

`jetstream` 是 Bookstore 教學環境的明確初始化工具，需要真實 NATS；不屬於上述無外部服務的入門範例。

`transactions` 是使用真實 PostgreSQL 的 [交易傳播教學](transactions/README.md)，需先準備 Bookstore 資料庫與 PANGOLIN_DSN。以 `python scripts/docs_examples.py --database` 在獨立 consumer 專案實際執行；一般命令則仍編譯該程式與組織組裝指南，不假報資料庫操作成功。
