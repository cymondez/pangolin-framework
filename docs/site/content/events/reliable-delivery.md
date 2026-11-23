---
title: 組裝可靠事件配送
description: 將本機事件、Outbox、Inbox 與 PostgreSQL 工作單元接在一起。
---

先完成[事件格式範例](integration-events.mdx)。本頁適用你自己的 PostgreSQL／NATS 應用；需要可用的 trusted Scope、部署好的 Events schema，以及具有最小權限的業務 Pool／佇列 Pool。

## 先接上完成階段

只建立 Publisher 不會自動加入交易。以下是應用 `setup/events.go` 的完整組裝函式。`appPool` 執行受完整 owner RLS 保護的業務交易；`queuePool` 用於認領／接收／重試 infrastructure 工作。兩者指向同一業務 DB，不承諾跨 DB 原子性。

```go
package setup

import (
    "time"
    "github.com/cymondez/pangolin-framework/events/distributed"
    "github.com/cymondez/pangolin-framework/events/local"
    "github.com/cymondez/pangolin-framework/events/outbox"
    eventpg "github.com/cymondez/pangolin-framework/events/postgres"
    "github.com/cymondez/pangolin-framework/persistence/postgres"
    "github.com/cymondez/pangolin-framework/persistence/uow"
    "github.com/jackc/pgx/v5/pgxpool"
)

func EventServices(appPool, queuePool *pgxpool.Pool, registry *distributed.Registry,
    bus *local.Bus, retry outbox.Policy) (uow.UnitOfWorkManager, *outbox.Publisher, *eventpg.Store, error) {
    store, err := eventpg.NewStore(queuePool, retry)
    if err != nil { return nil, nil, nil, err }
    publisher, err := outbox.NewPublisher(registry, store)
    if err != nil { return nil, nil, nil, err }
    resource := &eventpg.Resource{
        Base: &postgres.Resource{Pool: appPool}, Local: bus, Publisher: publisher,
    }
    manager, err := uow.NewManager(resource, uow.DefaultOptions{
        DefaultTimeout: 5*time.Second, CleanupTimeout: time.Second,
        HookTimeout: time.Second, MaxRequiresNewDepth: 2,
    })
    if err != nil { return nil, nil, nil, err }
    return manager, publisher, store, nil
}
```

將檔案放進自己的 Go module，執行 `go build ./...` 可檢查組裝。建立 Registry 時明示註冊 `distributed.Schema{Type, Version, Required, Validate}`；已發布的 type／version 不重寫。retry 的 Lease、SendTimeout、RetryInitial、RetryMax、MaxAttempts 都需正值，JitterFraction 為 0–1。

在可寫 UOW callback 內先保存業務，再 `publisher.Publish(ctx, envelope)`。Envelope 的 Tenant／完整 Scope 必須等於可信 binding。沒有可寫交易或沒有上述 Resource 接線就拒絕，不會改成直接 Send。

## Local Event

`local.NewBus(Limits, handlers...)` 封存註冊；Handler 的 Event 是確切 Go 型別範例，Priority 較小先執行，同值按註冊順序。將 Bus 傳入 Resource，業務內使用 `bus.Enqueue`。

完成時依 FIFO 迭代排空新事件並 flush；超過 MaxEventsPerUOW／MaxDispatchDepth 或 Handler 失敗使整筆交易失敗。SaveChanges 只 flush，不派送本機事件或提交。不要在本機 Handler 執行無法回滾的外部副作用。

## Outbox Worker

`outbox.NewWorker(store, broker, retry, workerName, authorize)` 的 authorize 需依目前服務身分與 owner grant 驗證。Worker 的 Dispatch 以短交易認領，每次產生新 lease token，在交易外 Send；只有持久化與路由確認成功才標 sent。逾期 Worker 的舊 token 不能更新新租約結果。

NATS Stream 必須使用 FileStorage，subject／stream／durable consumer 需一致。Send 的 publish ACK 只表示 Broker 已可靠接受，不表示 Consumer 的業務完成。連線與 Pool 各由一個 Module 負責 Close；啟停先停止認領、Drain，再關閉相依資源。

## Inbox Consumer

`inbox.NewReceiver(store, registry, group, verify, alert)` 先驗完整 Envelope 與可信 producer，持久化後才呼叫傳入的 ACK。verify 必須由 Broker ACL／session／route 的可信資訊判斷，不能照抄 Envelope.Producer。同 group／EventID 去重；同 ID 不同內容回衝突並告警。

`inbox.NewWorker(store, manager, group, bind, handler, maxAttempts, retry)` 的 bind 每次建立新的服務 context，重新驗證當前 Scope。Worker 在同一業務交易持有 Inbox 行鎖，執行 handler 與 processed；授權拒絕、已確認回滾都有界延期／隔離，避免無法處理的第一筆阻塞後續工作。Commit Unknown 不宣稱失敗或重設狀態。

Event／Job 可用 `distributed.Execution.Run` 重新綁定服務身分與 Scope，並在新根 admission 後重驗 action；代理型 Job 另驗使用者目前資格。這個執行契約沒有提供完整持久化 Job 排程器。

## 重送與保留

配送是 at-least-once。外部副作用使用 `inbox.ExternalKey(group, event, operation)` 的穩定鍵及支援冪等的 Provider，或再寫 Outbox；Inbox 不保證端到端 exactly-once。

人工重送透過 `eventpg.NewMaintenance(...).ReplayDead`，要求可信 owner、獨立 Permission 與同交易 Audit，保留 EventID、版本及完整 Envelope；processed 不能重設。Purge 只清理超期 sent／processed，pending／dead 保留。Inbox retention 需涵蓋 Broker 保留與人工重送窗口。

下一步：[Command 冪等](../data/idempotency.md)，處理使用者重試與提交結果未知。
