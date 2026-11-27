# ADR08 — Module 與發行基線

狀態：module／開發工具鏈已選定；發布尚未啟用。對應 ENV01、ENV04。

依使用者提供的 origin `https://github.com/cymondez/pangolin-framework.git`，固定 module 為 `github.com/cymondez/pangolin-framework`。Go 語言基線 1.27.0，測試 toolchain 固定 1.27.4；目前只承諾 linux/amd64 與 windows/amd64 的驗證目標，其他平台需另加相同 gate。

CI 使用 Go 1.27.4 官方 bookworm image 的 index digest；本機工具版本另記證據。Linux runner 執行 race；Windows 沒有 C compiler／CGO 時，明確記錄未執行，不偽稱通過。尚無發布用 Runtime／CLI，不填虛假的產品已發布版本。

未來 v0.1.0 的 Runtime、CLI、Template 綁同一 release manifest／commit；Event schema 維持自身版本。發布前需完整公開 API、全部適用案例、供應鏈檢查與乾淨重建。M0 CI 配置不等於遠端 workflow 已執行，也不自動 push 或發布。

依據：[Go 官方下載](https://go.dev/dl/)。
