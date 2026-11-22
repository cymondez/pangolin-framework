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
| 工作單元 | Required、ReadCommitted、交易內唯讀／讀寫 | RequiresNew、非交易寫入尚不支援 |
| PostgreSQL | pgx Pool、受控 Exec／QueryRow、Tenant RLS | 多列 Query／Batch 尚未提供 |
| 登入驗證 | RS256 JWT／JWKS | 自行設定可信 issuer／JWKS；不提供完整登入 UI |
| 多租戶 | tenant Scope | Enterprise／Organization Scope 尚未提供 |
| 事件 | Envelope 編解碼、JetStream Adapter、Bookstore Outbox／Inbox 範例 | 目前需程序重啟恢復斷線；不提供通用任意業務的 Outbox 自動接線 |

CLI 專案產生器、組織管理、完整平台服務與其他 DB／Broker Adapter 目前尚未提供。請先依[安裝頁](../getting-started/installation.md)建立普通 Go 專案，再選擇需要的公開套件。

Go 使用 1.27.4；真實資料範例使用 PostgreSQL 18.7 與 NATS 2.15.3。初次學習核心功能時不需要安裝這兩個服務。
