# Command 冪等與結果對帳

`New(Config, authorize)` 明示最大 key／body 長度、重試窗口、結果保留期限及結果 Protect／Unprotect。敏感結果使用部署的加密器；非敏感結果可注入明示的原樣編碼函式。保留期限不得小於安全重試窗口。部署透過 `Migrations` 的 `migrations` 子目錄安裝 Goose schema。

在可寫 UOW 內呼叫 `Execute(ctx, operation, key, canonicalJSON, work)`；完整 Tenant／Scope／Principal（kind／ID）／operation／key 唯一約束在 work 之前保留，同交易完成結果。相同內容回傳原結果，異內容 `IDEMPOTENCY_CONFLICT`，持有者回滾讓等待者取得 key。Manager 不重跑 callback。等待逾時保留 `IDEMPOTENCY_IN_PROGRESS` 與 cause，透過新根同 key 對帳。任何 Execute 失敗會標記根 rollback-only；Panic 保留原值重拋，外層即使捕捉錯誤或 recover 也不能提交 pending key。JSON 物件鍵排序不影響指紋；數值沿用 JSON number 字面值，應用需在第一次呼叫前完成自身 DTO 的標準化。

`Query` 在新的已授權 read UOW 查詢；查無結果回 `unknown`，不宣告失敗，過期明示 `expired`。每次 Execute／Query 都檢查目前 action 權限，RLS 及完整 key 限制 owner／principal。撤權不刪除已提交效果與防重紀錄。過期資料由受控維護流程清理，過期後不承諾同 key 防重。

套件不保存 context 或 Pool，沒有獨立關閉責任。`go test ./persistence/idempotency`、`go test -tags=integration ./tests/m3` 驗證真實唯一約束競爭與獨立連線對帳。
