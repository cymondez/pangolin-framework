# ADR11 — M1–M6 的產品版本固定為 0.1.0

日期：2026-11-21。依使用者明確指定，M1–M6 都是同一產品版本 0.1.0 的實作里程碑，不能將里程碑編號當成套件版本。根目錄 [VERSION](../../VERSION) 為產品版本的權威來源；現行規範與指南統一使用完整的 v0.1.0。

framework 自有 Module、後續 CLI／Template、官方使用文件與 Docker 產品標籤都對齊 VERSION。Go modules 的正式發行 tag 使用 v0.1.0；在 M6 發布 Gate 通過前不建立正式發行 tag，也不把版本號當成全部功能已完成的證據。目前仍為 0.1.0 開發中，已完成能力依同 commit evidence 判斷。

文件基線 v1.3、Go／PostgreSQL／NATS／第三方依賴版本、資料列 optimistic version、事件 schema_version 各有獨立意義，不改成 0.1.0。驗證 SemVer 範圍／不相容版本的負測試值同樣保留，歷史 evidence 的原始文字與 hash 不改寫。

`scripts/product_version.py` 盤點 VERSION、Bookstore 自有 Module 與 baseline Module fixture，納入 m0.py check／M1 累積 Gate；後續新增發行產物須使用同一版本來源。官方文件選單按套件版本分類，M2–M6 持續更新 0.1.0 文件，不新增名為 M2／M3 的產品版本。
