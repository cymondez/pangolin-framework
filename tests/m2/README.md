# M2 驗收

跨套件真實 PostgreSQL 驗收，配合 `tests/contract/m2.json` 的 133 個具名子情境映射。一般 Go 單元測試不啟動外部服務；`integration` build tag 的必跑案例缺少連線設定即失敗，不使用 skip。

- `uow_test.go`：Required／RequiresNew、交易 ID、提交後獨立連線對帳、Pool／鎖等待、Rows、版本競爭與取消。
- `commit_proxy_test.go`：解析真實 PostgreSQL frame，精確截斷 COMMIT 送達前／DB 完成回覆後，不依賴 sleep 猜測故障時點。
- `stages_test.go`：提交前階段的真實 SQL、Panic／deadline、DB 注入的 serialization／deadlock SQLSTATE 與明示上層重試。
- `scope_test.go`：同 User 兩 Org／Enterprise 的 Bookstore／FORCE RLS 薄片與 Scope 切換拒絕。SQL Authority 是測試 fixture，完整治理仍由 M3 交付。
- `stream_test.go`：持續 SSE 連線、兩個短交易與等待期間 Pool 無占用。
- `test_gate.py`：真實 import graph、tenant-only build、公開交易控制邊界及驗收映射反例。

連線與完整 Gate 見 [M2 開發指南](../../docs/guides/m2-development.md)。以本次專用 Compose project 執行，不使用共用資料庫。測試 ID 隨機；結束清除自己的容器、network、volumes，保留 workspace evidence。
