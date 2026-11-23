---
title: panGOlin 使用指南
slug: /
description: 將 panGOlin 加入你的 Go 專案，從第一個程式學會模組、依賴注入、資料與事件。
---

panGOlin 是用來開發 **Go 應用程式**的框架。你可以用它組織功能模組、傳入服務依賴、管理交易，以及隔離不同租戶的資料。

如果你會寫基本的 Go 程式，想讓逐漸變大的應用仍容易理解，可以從這裡開始。不需要先學會資料庫、訊息系統或領域驅動設計。

## 你可以用它做什麼？

| 你的需求 | 使用的功能 | 從哪裡開始 |
|---|---|---|
| 把帳號、訂單、商品等功能分成模組 | Module 與應用啟停 | [建立第一個應用](getting-started/quickstart.mdx) |
| 替換服務實作，讓業務程式容易測試 | 依賴注入（DI） | [注入你的服務](fundamentals/dependency-injection.mdx) |
| 在本機與部署環境使用不同設定 | Configuration | [讀取應用設定](fundamentals/configuration.mdx) |
| 一次完成多個資料寫入，失敗時一起回滾 | 工作單元（UOW） | [使用交易](data/unit-of-work.md) |
| 讓不同客戶只能存取自己的資料 | 身分驗證、Tenant Scope 與 RLS | [多租戶應用](security/multi-tenancy.md) |
| 把資料變更交給背景程序處理 | Integration Event、Outbox／Inbox | [使用事件](events/integration-events.mdx) |

## 第一次使用的建議順序

1. [安裝與建立專案](getting-started/installation.md)：把套件加入自己的 Go module。
2. [第一個應用](getting-started/quickstart.mdx)：執行完整程式，看到 `Hello, panGOlin!`。
3. [循序教學：可設定的問候服務](tutorials/greeting-application.mdx)：把設定、服務與模組組合起來。
4. [Bookstore 教學](tutorials/bookstore.md)：實際呼叫有登入驗證、資料庫與背景事件的 API。

已經知道要用哪個功能？直接從[套件使用範例](samples/index.md)選擇可以執行的程式。

需要可靠背景任務時，接著閱讀[持久化工作](platform/jobs.md)；需要追查失敗與積壓時，使用[觀測與 Health](platform/observability.mdx)。

## 使用這一版前

這是 **0.1.0 預覽版**。目前可使用本文列出的核心功能與範例；Required／RequiresNew 與進階隔離選項已提供；可選組織治理、當前資格授權及通用可靠事件已提供；持久化背景工作、容量控制及觀測已提供；自動建立專案的 CLI 尚未提供。採用前請查看[功能相容性](reference/compatibility.md)。
