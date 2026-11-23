---
title: 選擇版本與升級
description: 為應用固定套件來源，並在升級時檢查 API、設定與資料相容性。
---

目前 0.1.0 是預覽套件。頁首選單用來選擇套件對應的文件；有正式歷史版本後，你可以保留舊版指南繼續閱讀。

## 預覽期間如何固定依賴？

安裝頁使用本機 replace 指向套件來源。採用前請記錄該 checkout 的 commit，讓團隊使用同一份程式；不同的預覽來源可能包含 API 變更。

正式版本發布後，可以移除本機 replace 並使用對應的 Go module tag。當前還沒有正式 tag，請先依[安裝頁](../getting-started/installation.md)操作。

## 升級應用前

1. 選擇目標版本的文件，查看 API 與必要設定差異。
2. 在自己的測試環境更新依賴，重新建置並執行應用測試。
3. 使用資料庫或事件時，先確認資料表與事件 payload 相容。
4. 驗證主要使用情境與回復方式後，再部署應用。

套件版本與事件的 SchemaVersion 分別管理。升級框架不代表可以直接改寫已被其他服務使用的事件格式。

## 更新較早的 0.1.0 預覽來源

UOW sentinel 名稱已對齊 UOW_REQUIRED／UOW_CLOSED／UOW_BUSY，取代較早的 UOW_SCOPE_REQUIRED／UOW_SCOPE_ENDED／UOW_CONCURRENT_USE。已提交後的觀察器錯誤只記錄，不再使 Within 返回業務失敗。

Bookstore 的資料表新增 nullable deleted_at，app role 需要 UPDATE 權限；tenant policies 同時比對完整 Scope，Worker 也要更新 transaction-local 設定。不要只更新一半的 schema 或程序。保留資料時先備份並規劃明示的部署遷移；API 啟動不自動改表。

只使用 disposable demo 時，可先停止自己的 API／Worker，再確認 demo project 歸屬並清除其 volumes，然後重新執行教學準備步驟。這會刪除該 demo 的書籍資料，不能用在正式資料庫：

```powershell
$env:PANGOLIN_M1_PG_PORT = "15542"
$env:PANGOLIN_M1_NATS_PORT = "14322"
$env:PANGOLIN_M1_IDP_PORT = "18180"
docker compose -p pangolin-bookstore-demo -f deploy/compose/m1.yaml down --volumes --rmi local
pwsh ./examples/bookstore/scripts/demo.ps1 up
```

Runtime Options 新增 StartupChecks，請將 positional literal 改成具名欄位。使用可撤權的 Tenant Resolver／當前 Authority，Provider.Authorize 傳入 context 中相同的可信 binding。ScopeAware schema 升級、完整 inventory 與舊程序拒絕條件見[部署遷移](../data/migrations.md)；通用事件表與 Bookstore 薄片分開，需明示改接 Resource／Publisher／Worker。

未知 Scope Kind 在預設 Bookstore 由舊的 403 改為 400／SCOPE_KIND_UNSUPPORTED。組織範例改用獨立 ScopeAware Repository，不能直接沿用 TenantOnly 資料表。
