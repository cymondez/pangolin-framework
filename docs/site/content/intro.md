---
title: panGOlin 使用指南
slug: /
description: 為 Go 工程師設計的模組化應用框架入門導覽：建立心智模型、理解邊界設計，並一步步將 panGOlin 導入你的專案。
---

**panGOlin** 是一個為中大型 **Go 應用程式**設計的模組化應用框架。它協助團隊先以「**模組化單體（Modular Monolith）**」穩健交付，同時用明確的介面契約、交易邊界、多租戶隔離與可靠事件，保留未來按模組拆分服務的彈性。

如果你已經會寫基本的 Go（struct、interface、`context.Context`、`error` 處理），但不想讓專案隨著功能增加變成一團全域變數、到處傳 `*sql.Tx` 與混亂 import 的大泥球，這份手冊就是為你寫的。

---

## panGOlin 幫你解決什麼問題？

在沒有框架約束的 Go 專案裡，隨著團隊人數與業務增加，往往會遇到幾個共同痛點。panGOlin 用清楚、可驗證的機制逐一對症下藥：

| 專案成長後的痛點 | panGOlin 的解法 | 從哪裡開始學 |
|---|---|---|
| `main.go` 越來越大、模組互相亂 import、啟動與停機順序一團亂 | **Module DAG 與生命週期**：編譯期宣告模組相依，自動拓撲排序啟動、反序優雅停機 | [建立第一個應用](getting-started/quickstart.mdx) → [功能模組](fundamentals/modules.md) |
| 服務彼此直接 `New` 造成難以測試，或全域變數暗藏狀態污染 | **建構子 DI 與可見性邊界**：未 Export 的服務對他模組不可見，支援 Singleton／Transient／Operation 存活期 | [依賴注入](fundamentals/dependency-injection.mdx) |
| 本機、測試、正式環境設定散落各處，啟動後才發現少設環境變數 | **分層組態與啟動驗證**：`預設 < 檔案 < 環境 < CLI`，缺必要設定於啟動前立即失敗 | [應用設定](fundamentals/configuration.mdx) |
| 跨服務呼叫被迫層層傳遞 `*sql.Tx`、子服務出錯被吞掉導致半套資料提交 | **工作單元（UOW）與高階 Façade**：以 `application.CommandResult`／`Within` 獨占交易生命週期，支援 `Required`／`RequiresNew` | [使用交易與工作單元](data/unit-of-work.md) |
| 業務邏輯綁死特定資料庫驅動，換資料庫或寫測試成本極高 | **中立資料存取 Port**：統一透過 `persistence/database`，可選 PostgreSQL／MySQL／SQLite Adapter | [中立資料存取與資料庫選擇](data/database.md) |
| 多租戶 SaaS 容易漏寫 `WHERE tenant_id = ?`，或被請求 Header 偽造身分 | **可信 Context + 權限 + RLS 雙重防線**：憑證驗證、Tenant／Scope 解析、明確拒絕優先與資料庫 RLS | [可擴充登入](security/authentication.md) → [多租戶](security/multi-tenancy.md) |
| 寫完資料庫再直接發訊息，發生「DB 回滾但事件已送出」的資料分裂 | **Local Event + Outbox／Inbox**：事件與業務寫入同交易保存，由背景 Worker 可靠配送並去重 | [事件格式](events/integration-events.mdx) → [可靠配送](events/reliable-delivery.md) |
| 依客戶／組織／使用者動態調整偏好、配額或快取，卻不小心跨租戶污染 | **分層 Settings、Features 與 Scoped Cache**：5–7 層繼承解析、Feature 絕不越權、快取鍵自動帶完整 Scope | [分層設定與 Features](platform/settings.mdx) → [快取](platform/cache.md) |
| 每次開新專案都要手動複製上百行啟動組裝與設定檔 | **pangolin CLI**：一鍵生成具登入、資料庫與租戶隔離的獨立專案，並支援模組骨架與診斷 | [用 CLI 建立自己的 Bookstore](tutorials/generated-bookstore.md) |

---

## 新進人員導入的建議學習路線

建議依照下面 5 個階段循序推進，**第 1–3 步只需要 Go 與 Git，不需要 Docker 或資料庫**：

```text
[階段 1：零外部依賴起步（約 20 分鐘）]
  1. 安裝與建立專案 ──▶ 2. 第一個應用 (hello) ──▶ 3. 認識框架架構總覽
          │
          ▼
[階段 2：掌握核心組裝（約 30 分鐘）]
  4. 可設定的問候服務 ──▶ 5. 功能模組與依賴注入（含多模組 Contract 範例）
          │
          ▼
[階段 3：跑通完整資料庫與安全流程（需 Docker）]
  6. Bookstore 完整 API ──▶ 7. 交易傳播實作 (Required vs RequiresNew)
          │
          ▼
[階段 4：導入自己的真實專案]
  8. 用 CLI 生成獨立專案 ──▶ 9. 依需求深入資料存取、多租戶、可靠事件與背景工作
```

1. **[安裝與建立專案](getting-started/installation.md)**：建立獨立的 `hello-pangolin` Go 專案並接上本機套件來源。
2. **[第一個應用](getting-started/quickstart.mdx)**：寫出第一個模組並透過 `app.Run` 啟動與優雅停機。
3. **[安排你的應用程式（架構總覽）](concepts/architecture.md)**：先建立全站共用的心智地圖——看懂分層職責、啟動生命週期與單一請求管線。
4. **[建立可設定的問候服務](tutorials/greeting-application.mdx)**：動手結合 DI 建構子註冊、設定預設值與環境變數覆寫。
5. **[Bookstore 教學](tutorials/bookstore.md)**：啟動真實 PostgreSQL、NATS 與示範登入服務，實際體驗交易、跨租戶 403 攔截與背景事件投影。
6. **[用 CLI 建立自己的 Bookstore](tutorials/generated-bookstore.md)**：改用 `pangolin` CLI 生成可直接開發的獨立專案骨架。

> **兩種 Bookstore 範例的小提醒**：閱讀教學時請留意區分——
> - **套件內既有範例**（`examples/bookstore`，見 [Bookstore 教學](tutorials/bookstore.md)）：建立書籍回傳 **HTTP 201**，回應 JSON 含 `version` 欄位，並搭配獨立事件 Worker。
> - **CLI 生成的獨立專案**（見 [用 CLI 建立自己的 Bookstore](tutorials/generated-bookstore.md)）：建立與讀取都回傳 **HTTP 200**，回應欄位精簡為 `id` 與 `title`，方便作為新專案的乾淨起點。

已經熟悉基礎、只想直接跑某個功能的範例程式？直接前往 **[套件使用範例總覽](samples/index.md)**。

---

## 本版本（0.1.0 預覽版）支援現況

本文件對應 **0.1.0 預覽版**（目前尚未發布遠端正式 `v0.1.0` tag，安裝方式採標準 Go module 搭配本機 `replace`）。

- **已完整提供並可直接使用**：Root 模組裝配與建構子 DI、組態分層與優雅停機、UOW（`Required`／`RequiresNew`）與高階 `CommandResult`／`QueryResult`、PostgreSQL／MySQL／SQLite 中立資料存取、可擴充身分驗證與多租戶／可選組織治理、Local Event 與 Outbox／Inbox 可靠配送、持久化背景工作（Jobs）與容量背壓、OpenTelemetry 觀測與健康檢查、分層 Settings／Features／Scoped Cache、可選 SignalR 即時通訊，以及 `pangolin` CLI。
- **使用邊界**：公開文件僅介紹目前版本實際可執行並經驗證的功能。導入正式專案前，請務必查閱 **[功能相容性與支援邊界](reference/compatibility.md)** 確認各資料庫與模組的支援範圍。

---

## 設計靈感與致謝

panGOlin Framework 是以 Go 語言獨立設計並從零實作的開源專案，未複製或移植任何外部框架的原始碼。在模組化單體架構、工作單元、多租戶與分層設定等高層設計概念上，本專案受到開源社群與 [ABP Framework](https://abp.io/) 等企業級應用框架的理念啟發，謹此致謝。

panGOlin Framework 與 ABP Framework（VoloSoft）之間無任何官方隸屬、合作、授權或背書關係；相關名稱僅作概念參考與致謝之用，商標歸屬各自原權利人。
