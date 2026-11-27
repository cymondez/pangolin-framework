# ADR01 — Package 與公開 Port

狀態：採用於 M0；產品契約未驗收。對應 ENV04、ARC-001、A45。

採實作規範 §1、§10 的單一 module 結構，公開 API 分別位於 `kernel/*`、`persistence/*`、`security/scope`、`events/*`。組裝由 constructor／typed provider 完成。Domain 僅依賴標準庫及薄基元；Application 使用 Domain／Port；具體 DB、Broker、HTTP 實作置於 Infrastructure。Core 不依賴 organization 實作。

開發檢查置於 `internal/quality`、`tools/check`；本機測試服務置於 `internal/testkit`。不為空目錄建立假功能或萬用 CRUD。公開介面先按對應需求建立契約測試再實作。M0 已加入 import graph gate 與禁止依賴反例回歸；M1 將實際模組、範例與生成物納入同一規則。

選擇理由：保持現有契約可追蹤；目錄不是完成證據。Builder／DI／Module／Runtime 的具體 Port 位於對應 contracts.go，API surface 存於 tests/contract/api.json；實作在 M1。驗收：A45 注入禁止 import 時 CI 失敗，合法圖可編譯。
