# 共用測試 fixture

此目錄只放可供不同框架／範例測試共用的非公開 fixture，不供 production 使用。

- `idp/` 與 `cmd/idp/`：無驗證的本機測試 IdP，簽發真實 RSA JWT；Compose 建置來源保持不變。
- `assemblies/tenant`、`organization`、`replacement`：三種 Profile 的契約 fixture；不冒稱完整 Organization Runtime 已交付。

Bookstore 專用 crash-worker／m1-query 已歸 [examples/bookstore/internal/testkit](../../examples/bookstore/internal/testkit/README.md)。正式產品入口在各擁有者的 cmd，測試結果寫 artifacts。驗證入口與 fixture 的使用範圍見 [測試索引](../../tests/README.md)。

M5 的 cmd/template-db 僅接受 m5_template_* 專用資料庫名稱，提供 create／drop／reconcile：獨立管理連線對帳生成物的 business／Audit／Outbox／Idempotency 記錄、FORCE RLS、應用角色權限與 Profile schema。它不是產品 CLI，逐檔列入 test fixture 排除 manifest。
