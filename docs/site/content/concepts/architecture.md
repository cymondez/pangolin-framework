---
title: 安排你的應用程式
description: 將自己的 HTTP、業務服務與資料存取分開，並在應用入口組裝。
---

第一個程式可以只有 main.go。功能增加後，再依責任分開，讓業務規則與 HTTP／資料庫實作容易各自修改。

## 一個可以採用的專案結構

```text
my-app/
  cmd/api/main.go          應用入口與組裝
  internal/books/
    domain/               書籍與業務規則
    application/          建立、查詢等使用情境
    infrastructure/       Repository 與 SQL
    transport/http/       HTTP handler
  go.mod
```

這是你自己應用的範例結構，可依大小調整。panGOlin 的套件由 import 引用，不需要複製進你的專案。

## 每一層要寫什麼？

| 層 | 以建立書籍為例 |
|---|---|
| HTTP handler | 解碼請求、驗證身份、呼叫服務、回應狀態 |
| Application | 檢查操作權限、開始 UOW、呼叫 Repository 與事件 publisher |
| Domain | 書名等業務欄位與不變條件 |
| Infrastructure | 執行參數化 SQL、保存 Outbox、接入具體服務 |
| 應用入口 | 選擇實作、註冊 Provider、建立 Host |

Application 依賴小介面，具體 SQL／HTTP 放在邊緣。需要替換實作或測試業務時，就能從 constructor 傳入不同服務。

## 參照可執行的程式

[Bookstore](../tutorials/bookstore.md)使用同樣的分層；套件來源中的 `examples/bookstore` 是範例應用，可以先從 application/books.go 追到 Repository，再看 HTTP handler。

還不需要資料庫時，先使用[問候服務](../tutorials/greeting-application.mdx)的單檔方式，等到功能變多再拆資料夾。
