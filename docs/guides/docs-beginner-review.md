# 官方網站的初學者走查

日期：2026-11-22。讀者假設：能寫基本 Go，第一次接觸 panGOlin；沒有讀過本倉庫 spec、里程碑或維護 README。這份走查是維護紀錄，不放進公開網站。

## 章節設計與參考

參照 [ABP 官方文件](https://abp.io/docs/latest/) 的 Get Started、Tutorials、Framework Fundamentals／Infrastructure 與參考分類，改為開始使用、循序教學、套件使用範例、基礎功能、資料存取、身份／多租戶、事件、應用架構與參考。

不照搬 ABP 的功能範圍或 .NET API。公開內容只教使用者目前可以呼叫的 panGOlin 功能；沒有正式 tag 的預覽安裝採普通 Go module＋本機 replace，避免虛構已發布的 go get 命令。

## 從零操作檢查

| 使用者問題 | 修正與實際檢查 |
|---|---|
| 我需要哪些工具？ | 核心起步僅 Go／Git；Docker／PowerShell 到 Bookstore 才需要 |
| 這是在改 Framework，還是在建立我的應用？ | 安裝頁建立獨立 hello-pangolin，說明與套件來源並排的目錄 |
| 我要在哪裡執行命令？ | 每個段落明示 working directory，Bookstore 明示三個終端與變數範圍 |
| 程式需要自己補 import 或未定義變數嗎？ | 六個完整 main.go 從實際來源同步顯示；資料層完整檔案另做編譯檢查 |
| 執行完應該看到什麼？ | 每個入門程式附預期輸出，環境覆寫與非法設定也實際執行 |
| 我必須先理解規格／Gate 嗎？ | 公開導覽、首頁與版本頁已移除 spec／驗收／內部里程碑與討論內容 |
| 怎麼跑有資料的範例？ | 提供 demo.ps1，明確初始化自己的 DB／Broker，不要求使用者呼叫內部測試 harness |
| 怎麼繼續與怎麼關閉？ | 第一個應用→問候服務→Bookstore；各頁有下一步，服務有明確停止命令 |

## 已執行的使用者操作

`python scripts/docs_examples.py` 建立獨立 consumer module，使用公開套件執行 hello、greeting-app、configuration、dependency-injection、lifecycle、event-envelope。六份程式、APP_NAME 覆寫、空設定失敗，以及手冊中的 Service／Repository 完整 Go 檔案均通過；記錄位於 `artifacts/docs/beginner-examples.json`，保留 `.cache/docs-beginner-*` 供重看。

依 Bookstore 教學實際執行隔離 demo up、API 與獨立 Worker，取得簽章 Token：建立 201、讀回 200、跨租戶 403、未登入 401，Worker 已將同一筆書籍投影為一筆資料。操作記錄位於 `artifacts/docs/bookstore-user-flow.json`，未保存 Token。環境以自己的 project／network／volumes 與 loopback ports 啟動。

網站 production build 已通過 25 個靜態頁面、1097 個本機連結／錨點與本機搜尋檢查。Docker 以非 root Nginx、唯讀 filesystem 與 64 MiB 資源上限健康啟動；子路徑首頁／深層文件回應 200，無效路徑與前綴外路徑回應 404。

瀏覽器實際從首頁進入安裝、第一個應用，確認完整 main.go、執行結果與下一步；中文搜尋「依賴注入」能導向相應完整程式。1280px 桌面與 390px 手機皆無頁面水平溢出，手機導覽能找到開始使用／循序教學／使用範例。畫面保存為 `artifacts/docs/getting-started.jpg` 與 `mobile-navigation.jpg`。

隔離副本的原生快照建置通過 46 頁、2155 個站內連結與兩份版本搜尋索引。實際切換 0.1.0／Next 保留同一 UOW 教學；自動檢查另修改 current 的介紹與 Go 程式，確認已封存的文件、程式碼與搜尋均不被改寫，導覽／頁尾也不跳離所選版本。正式 checkout 仍只有 0.1.0 預覽，未建立發行 tag 或假歷史版本。

上述為本機文件／使用範例驗證，遠端 CI 僅新增設定、未宣稱已執行。後續依使用者要求清理已確認歸屬本專案的一次性環境：移除 8 個已停止容器、14 個測試 volumes、2 個測試 network 與 7 個無使用的專用測試 IdP 映像；紀錄保存於 `.cache/pangolin-test-cleanup-result.json`。其他專案維持原狀。

供使用者瀏覽的正式 `pangolin-docs` 本機服務保留於 `http://localhost:18088/`。已套用「晨光藍砂」配色，重新檢視桌面／手機首頁、文件、中文搜尋、手機導覽與手動明暗切換；首頁示意卡保持水平。建置仍通過 25 頁與 1097 個站內連結，畫面與檢查摘要保存於 `artifacts/docs/home-dawn.jpg`、`reading-dawn.jpg` 與 `dawn-qa.json`。這次配色調整沿用同一個 docs 服務，未建立一次性測試環境。

## 交易與 Scope 文件增量走查（2026-11-22）

在 M2 Runtime 同版 Gate 通過後更新公開文件，新增交易傳播循序教學與資源 Scope 擴充指南，並同步 UOW／PostgreSQL／相容性／升級／錯誤回應。導覽維持套件使用者視角，沒有新增內部里程碑或驗收入口。

`python scripts/docs_examples.py --database` 在新的獨立 consumer module 執行七份完整程式；交易範例使用真實 PostgreSQL，五項輸出皆為 true，證明 Required 共根、RequiresNew 新根、父回滾、子提交及舊 Scope 結束。手冊 Service／Repository／Organization 組裝完整檔案也編譯通過；預設驗證不需要 DB，資料庫操作必須明示 --database，不假報執行。

Native production build 與 Docker build 都通過 27 頁、1233 個站內連結及一份搜尋索引。原生版本快照／對應頁切換／current 與封存程式碼及搜尋隔離／子路徑檢查通過 50 頁、2431 個連結及兩份索引，正式 checkout 沒有建立發行快照。Windows 沙箱建立 security 輸出目錄曾遭 EPERM；以相同命令取得必要執行權限後已完整通過，沒有變更 ACL。

瀏覽器實際讀取新交易教學的前置條件、命令、輸出與完整程式；搜尋 RequiresNew 出現新教學與疑難排解，透過側邊欄到達資源 Scope 指南，確認完整治理尚未提供的邊界說明。兩頁在預設桌面 viewport 無頁面水平溢出；未因本次文字更新重做手機／配色測試。截圖為 artifacts/docs/m2-transactions.jpg 與 m2-scopes.jpg，摘要為 artifacts/docs/m2-qa.json。

既有 pangolin-docs 服務已重建為更新後內容，維持 `http://localhost:18088/`。

## 授權、治理與可靠事件增量走查（2026-11-23）

在 M3 Runtime 來源 `7f17ecc76a84e65ec4d64fb39b0900fb6bb50ba7` 通過乾淨同版雙平台 Gate 後，新增授權、組織治理、HTTP／稽核、可靠配送、Command 冪等與部署遷移六篇使用指南；同步 Tenant／Scope、相容性、升級、啟動與範例索引。公開導覽仍以套件使用者為讀者，不加入內部里程碑或驗收入口。

`python scripts/docs_examples.py --database` 在新的獨立 consumer module 實際執行八份完整程式，含 authorization 的三行拒絕／允許輸出與真實 PostgreSQL 交易五項結果。原手冊的 Service／Repository／Organization 組裝，以及新增冪等 Service、事件 Resource 與部署 Runner 完整檔案均編譯通過。結果保存在 `artifacts/docs/beginner-examples.json`。

Native／Docker production build 都通過 33 頁、1559 個站內連結／錨點及一份搜尋索引。原生版本快照、對應頁切換、current／封存內容與程式碼／搜尋隔離、子路徑回歸通過 62 頁、3095 個連結及兩份索引；正式 checkout 沒有建立發行快照。Native 建置首次遇到輸出目錄 EPERM，以相同命令取得必要執行權限後完整通過，沒有修改 ACL。

瀏覽器從首頁的使用範例進入新授權指南，確認操作目錄、完整 main.go、預期輸出、教學身分的邊界與下一步。搜尋 ownerless 能找到組織治理並跳到相關章節，實際讀取 ownerless／Recover 與替換 Provider 的說明，再進入可靠配送指南確認前置條件及完整組裝檔。桌面 viewport 1280px、documentWidth 1265px，沒有頁面水平溢出；本次未重做手機與配色測試。摘要保存於 `artifacts/docs/m3-qa.json`。

本次專用 PostgreSQL／NATS／IdP、network、兩個測試 volumes、Linux runner cache volume 及專用 IdP 映像已核對歸屬後清除，其他專案未受影響。既有 docs 服務已更新且健康，持續提供 `http://localhost:18088/`。
