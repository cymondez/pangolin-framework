# Book Domain

此套件只依賴 Go 標準庫。`NewBook(id, title)` 維護非空 ID／title、trim title、最多 200 bytes 與初始 Version=1；失敗為 `ErrInvalidBook`。Book 的公開 DTO 欄位為 id／title／version。

[Repository Port](book.go) 只提供 M1 的 Insert／Get，不暴露 Pool、Tx 或萬用 CRUD。查無資料／重複資源以 `ErrNotFound`／`ErrConflict` 表達，由 Adapter 轉換底層錯誤。完整更新／版本比較矩陣在 M2 交付。

Application 先建構合法 Book，再進入 UOW；Repository 不能代替 Domain 檢查。新增模型時維持標準庫邊界，不能 import Application、HTTP、pgx 或 Broker。

驗證：`go test ./...` 的架構檢查與 [M1 整合測試](../../../docs/guides/m1-development.md)；Domain／Application 禁止 import 的真實注入反例位於 `tests/m1/test_gate.py`。
