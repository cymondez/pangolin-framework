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

## 回應成功與重試

Within 回傳 nil 才能向呼叫者回應成功。若得到 `UOW_COMMIT_UNKNOWN`，表示提交結果無法確認；請保留操作 id，透過查詢或對帳確認結果，不要無條件重送寫入。

0.1.0 目前支援 Required、ReadCommitted 與交易中的唯讀／讀寫操作。RequiresNew、非交易寫入與其他隔離等級尚不支援，請先查看[相容性](../reference/compatibility.md)。
