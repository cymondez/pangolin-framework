# moduleapp/catalog：契約擁有者

本 package 擁有 `catalog/contracts`（`README.md` 見該目錄）的公開契約。`Module` 以 `Add(NewService, Singleton)` 註冊實作、以 Export 把 `contracts.Catalog` 介面契約暴露給其他模組；實作並未結構性自動暴露。`Service` 依賴 store 服務（建構子參數型別即解析鍵），並將讀寫轉送共用 store。

Start 透過受限 Resolver 解析自身 Export 的契約並寫入示範資料；Stop 無自有資源。
