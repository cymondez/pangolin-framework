# ADR12 — 版本化官方靜態文件網站

日期：2026-11-21。使用者確認採用 Docusaurus＋Markdown／MDX＋Nginx Docker，網站原始碼集中於 `docs/site/`；目前產品版本仍為 0.1.0。操作、維護與驗證見 [網站 README](../site/README.md)。

Docusaurus 提供原生文件快照與同頁版本選單；教學位於 `content/`，UI 位於站點自己的 `src/`，不改 Go package import path。既有 ADR、指南、evidence 與規格維持原本責任，不自動全部對外發布。M1 教學以實際 API 為準，M2–M6 明確標為規劃。

Node.js 只在開發／建置階段使用。執行產物為靜態 HTML／CSS／JavaScript，搭配本機中文／英文搜尋索引；使用 easyops-cn 的 Docusaurus 搜尋插件，不啟用外部 AI／搜尋服務。部署使用固定官方 digest 的 Node builder 與非 root Nginx，Compose 有獨立名稱／network、loopback port 與資源限制。

根目錄 VERSION 對齊網站 package／lockfile、build metadata、image tag／OCI label。M1–M6 不建立不同套件版本；正式發布 Gate 後才保存快照。獨立 fixture 驗證原生快照、current／archive 互不污染、同頁切換與版本搜尋，不在正式 checkout 虛構發行版本。

代價是需維護前端依賴與同步教學；版本快照僅在正式發布時保存，避免每個里程碑複製整套文件。CI 檢查網站建置、斷鏈與快照流程，不能代替 Framework 真實服務 Gate。

2026-11-22 依使用者再次明確指定，網站以套件使用者／初學者為對象，參照 ABP 的開始使用、循序教學、功能與參考分類。公開頁面移除規格、內部里程碑／Gate、證據與討論；維護操作留 README。使用範例為獨立 consumer Go module；建置時同步完整程式，MDX 以相對路徑顯示並隨正式文件快照封存，避免舊版引用新版程式；另於獨立應用專案執行並完成初學者走查。

依據：[Docusaurus 版本管理](https://docusaurus.io/docs/versioning)、[部署](https://docusaurus.io/docs/deployment)、[本機搜尋插件](https://github.com/easyops-cn/docusaurus-search-local)。
