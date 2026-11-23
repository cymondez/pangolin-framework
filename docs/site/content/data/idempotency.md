---
title: Command 冪等與結果對帳
description: 用同一個操作 key 安全重試，並處理提交結果未知。
---

付款、建單等 Command 可能在成功提交後失去回應。冪等 key 讓相同請求取得原成功結果。先準備 PostgreSQL UOW、可信 Scope、目前 action 授權，並安裝 `persistence/idempotency.Migrations`。

## 設定 Store

`idempotency.New(Config, authorize)` 要求 MaxKeyBytes、MaxBodyBytes、RetryWindow、Retention，以及 Protect／Unprotect 結果保護函式。Retention 不得短於 RetryWindow；敏感結果應使用部署的加密器，非敏感結果也要明示選擇編碼策略。authorize 每次依目前權限檢查 operation。

以下是你自己的 `orders/service.go` 完整檔案；Manager、Store 與業務 work 由 constructor 注入。work 只使用回傳的 UOW context，回傳安全且可重播的 JSON 結果：

```go
package orders

import (
    "context"
    "github.com/cymondez/pangolin-framework/application"
    "github.com/cymondez/pangolin-framework/persistence/idempotency"
    "github.com/cymondez/pangolin-framework/persistence/uow"
)

type Service struct {
    manager uow.UnitOfWorkManager
    keys *idempotency.Store
}

func New(manager uow.UnitOfWorkManager, keys *idempotency.Store) *Service {
    return &Service{manager: manager, keys: keys}
}

func (s *Service) Create(ctx context.Context, key string, body []byte,
    work func(context.Context) ([]byte, error)) ([]byte, bool, error) {
    var result []byte
    var replayed bool
    err := application.Command(ctx, s.manager, func(c context.Context) error {
        var err error
        result, replayed, err = s.keys.Execute(c, "orders.create", key, body, work)
        return err
    })
    return result, replayed, err
}
```

這個檔案可放進你的 Go module 後執行 `go build ./...`；實際呼叫前，入口仍須依[HTTP 與稽核](../security/http-and-audit.md)建立可信 context。

## 重試時保留什麼？

唯一鍵包含 Tenant、完整 Scope、Principal kind／ID、operation 與 key。DB 唯一約束在業務之前保留 key，業務／成功結果／Outbox 同交易提交；相同指紋取得原結果，不同內容回 `IDEMPOTENCY_CONFLICT`。併發等待者會等候持有者提交或回滾，不能用先查後寫取代唯一約束。

body 必須是合法 JSON。物件鍵順序不影響指紋，數值保留 JSON number 字面值；在第一次 Execute 前完成自己 DTO 的標準化。任何 Execute 失敗都使根 rollback-only，捕捉錯誤也不能提交 pending key。

## 收到 Unknown

保留原 key，以新的已授權 read UOW 呼叫 `Store.Query(ctx, operation, key)`：

| State | 應用應如何處理 |
|---|---|
| succeeded | Payload 是保護後解碼的原結果 |
| in_progress | 等候並以相同 key 有界重查 |
| unknown | 查無結果仍不能證明原操作未提交，繼續對帳 |
| expired | 已超過保留期限，不承諾相同 key 防重 |

查詢與重播都重新檢查目前權限。撤權不刪除已提交效果，也不能靠舊結果繞過撤權。外部付款或郵件另需穩定 Provider 冪等鍵，或透過二次 Outbox 派送。
