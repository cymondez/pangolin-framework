---
title: 0.1.0 功能相容性
description: 選擇適合目前預覽版的功能，了解現有 API 支援與限制。
---

本文件對應 0.1.0 預覽套件。採用某項功能前，可以先從下表確認適用範圍。

| 功能 | 目前可使用 | 使用前注意 |
|---|---|---|
| 模組與啟停 | 明確註冊、相依排序、啟動／停止 | 不提供動態熱插拔 |
| 依賴注入 | Singleton／Transient／Operation、明確替換 | Singleton 不能捕獲操作範圍服務 |
| 啟動設定 | 預設、檔案、環境、CLI 來源合併 | 來源讀取由你的應用提供 |
| 工作單元 | Required／RequiresNew、三種隔離級別、唯讀／讀寫、deadline 與完成通知 | 不提供非交易寫入、Savepoint、跨 DB 原子性 |
| PostgreSQL | pgx Pool、受控 Exec／QueryRow／Query、完整 Scope RLS | Rows 必須關閉；沒有 Batch 入口 |
| Application／HTTP | 明示 Command／Query、提交後回應、每次串流短操作 | 先建立可信身分與 Scope；HTTP Command 不加入外層 UOW |
| 登入驗證 | RS256 JWT／JWKS | 自行設定可信 issuer／JWKS；不提供完整登入 UI |
| 多租戶 | tenant Scope、中立 Kind Registry、可選組織 Provider／ScopeAware 範例 | 組織 Authority 須明示注入；完整治理／撤權整合尚未提供 |
| 事件 | Envelope 編解碼、JetStream Adapter、Bookstore Outbox／Inbox 範例 | 目前需程序重啟恢復斷線；不提供通用任意業務的 Outbox 自動接線 |

CLI 專案產生器、組織管理、完整平台服務與其他 DB／Broker Adapter 目前尚未提供。請先依[安裝頁](../getting-started/installation.md)建立普通 Go 專案，再選擇需要的公開套件。

Go 使用 1.27.4；真實資料範例使用 PostgreSQL 18.7 與 NATS 2.15.3。初次學習核心功能時不需要安裝這兩個服務。
