# Bookstore 教學環境

從 repository root 執行 `pwsh ./examples/bookstore/scripts/demo.ps1 up`。需要 Go 1.27.4、Docker／Compose 與 PowerShell 7，建立固定的 pangolin-bookstore-demo project：PostgreSQL／NATS／本機示範 IdP，初始化書籍 schema 與 BOOKSTORE_DEMO JetStream。

腳本先唯讀盤點 Engine、容器、Compose、volume／network 與資源，驗證同名 project 的 owner／config path，確認 loopback ports；只啟停自己的環境。`status` 查看，`down` 停止並保留 volumes。不操作全域服務或 prune。

重跑 up 保留既有 schema 與資料；此為使用教學初始化，不提供正式 migration 管理。密碼與 IdP 僅供隔離本機示範。正式 API 與 Worker 的實作／測試不因本腳本而改變。

使用者步驟見 [Bookstore 教學](../../../docs/site/content/tutorials/bookstore.md)。語法以 PowerShell Parser 檢查，完整 up／API／Worker／HTTP roundtrip 在隔離環境實際操作後記錄。
