---
title: 使用交易與工作單元
description: 在自己的應用服務中以 Within 包住一次完整寫入，處理回滾與提交結果。
---

當一次操作需要寫入多份資料時，你通常希望全部成功才保存；其中一步失敗就一起回滾。工作單元（Unit of Work，UOW）替你的應用管理這個邊界。

若還沒使用過資料庫，先完成 [Bookstore](../tutorials/bookstore.md)，觀察建立書籍與讀回的結果。

## 把交易放在應用服務

以下可以作為你自己應用的 `books/service.go`。BookWriter 是你的 Repository 介面，實作由 constructor 傳入：

```go
package books

import (
    "context"
    "github.com/cymondez/pangolin-framework/persistence/uow"
)

type BookWriter interface {
    Insert(context.Context, string) error
}

type Service struct {
    transactions uow.UnitOfWorkManager
    books BookWriter
}

func NewService(transactions uow.UnitOfWorkManager, books BookWriter) *Service {
    return &Service{transactions: transactions, books: books}
}

func (s *Service) Create(ctx context.Context, title string) error {
    return s.transactions.Within(ctx, uow.Options{Mode: uow.ReadWrite},
        func(work context.Context) error {
            return s.books.Insert(work, title)
        })
}
```

在[PostgreSQL](postgresql.md)組裝 Manager／Repository，並將[驗證過的租戶 context](../security/multi-tenancy.md)傳給 Create。Bookstore 的 Books.Create 使用相同方式，把書籍與待送事件包進同一筆交易。

## 每次操作要怎麼選？

| 操作 | Options.Mode |
|---|---|
| 只查詢資料 | `uow.ReadOnly` |
| 新增、更新、刪除 | `uow.ReadWrite` |

未指定時預設唯讀。Repository 只能使用 Within 傳入的 work；不要保存它供下一次請求使用。

## 呼叫另一個也使用 UOW 的服務

預設 Required 會加入目前的交易，因此外層完成前不會提交。內層失敗後，即使你捕捉錯誤，整筆交易仍會回滾。這能避免部分服務失敗卻只保存一半資料。

唯讀 Required 子範圍即使加入可寫根交易，也不能寫入。子 callback 結束後，其 context 失效；父範圍仍可繼續使用自己的 context。

需要獨立提交時明示 `Propagation: uow.RequiresNew`，它暫停父範圍並取得另一條連線。子已提交的資料不隨父回滾，也看不到父尚未提交的資料。先執行[交易傳播教學](../tutorials/transactions.mdx)，觀察兩種行為。

## 選項與期限

| 選項 | 使用方式 |
|---|---|
| TransactionAuto | 可寫操作自動開交易；使用 transaction-local RLS 的讀取也開唯讀交易 |
| Isolation | 可選 ReadCommitted、RepeatableRead、Serializable；Required 明示的級別必須與父交易相同 |
| Timeout | 零值使用根預設或繼承父剩餘期限；正值只能縮短期限，不能延長父操作 |

Manager 的 DefaultTimeout、CleanupTimeout、HookTimeout 與 MaxRequiresNewDepth 必須是正值。連線池取得、Begin、SQL、提交前階段與 Commit 都受有效 deadline 限制。RequiresNew 等待第二條連線或父鎖時也會逾時，不會改用父交易。

非交易唯讀只適用於不依賴 transaction-local RLS 的 Resource。這個 PostgreSQL Adapter 一律要求交易；非交易寫入、Savepoint、Suppress 與跨 DB 原子提交不提供。

## SaveChanges 與完成通知

SaveChanges 只 flush，不提交、不排空本機事件、不執行完成回呼。pgx 的 SQL 通常已即時送入目前交易，因此可能沒有待 flush 資料；整筆交易之後仍能回滾。

OnCompleted 在已確認成功且資源釋放後依註冊順序執行。OnFailed 接收失敗 Outcome；OnDisposed 在清理後通知一次。每個觀察器有獨立 HookTimeout，錯誤或 Panic 會記錄並繼續後續通知，不把已提交交易改成失敗。回呼拿不到原 UOW Executor；可靠後續工作要寫入 Outbox。

## 回應成功與重試

Within 回傳 nil 才能向呼叫者回應成功。若得到 `UOW_COMMIT_UNKNOWN`，表示提交結果無法確認；請保留操作 id，透過查詢或對帳確認結果，不要無條件重送寫入。

Manager 不自動重跑 callback。只有上層明確允許重試、已確認回滾且有界退避時，才用新的根操作重跑整個 Use Case。HTTP Command 可使用共用 `transport/http.Command` 延後 2xx；它拒絕加入既有外層 UOW。串流連線則每次工作各呼叫 `StreamOperation`，不以一個長交易包住整條連線。

詳細支援邊界請查看[相容性](../reference/compatibility.md)。
