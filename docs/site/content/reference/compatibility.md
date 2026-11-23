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
| 多租戶與授權 | 當前 Tenant 資格、admission、Permission／Resource Policy、完整 Scope RLS | 權威故障拒絕；新根重新驗證；不接受 DTO 自行授權 |
| 組織治理 | 可選 PostgreSQL Authority、Management／HTTP、ownerless 復原與 Provider 匯入對帳 | 明示注入 Permission／Effects／inventory，父層無內容聯集 |
| 稽核與冪等 | 同交易必要 Audit／成功結果、獨立安全記錄、同鍵重播及 Unknown 對帳 | 結果保護與保留期限由部署明示設定 |
| 遷移 | Goose、全域 session 鎖、API 相容性檢查、ScopeAware 前向升級 | 部署與 API 角色分開，API 不執行 DDL |
| 事件 | 版本化 Registry、Local FIFO、通用 PostgreSQL Outbox／Inbox、租約 fencing、JetStream 持久確認 | 明示 Resource／Worker 組裝；at-least-once，外部副作用需冪等 |
| 背景工作 | 持久化單次／延後工作、版本 Registry、租約、取消、有界重試、Audit 重送 | 至少一次執行；Handler 需冪等，複雜 Cron 未提供 |
| 觀測與維運 | 明確注入 OpenTelemetry、JSON logs、Health、全域容量及積壓告警 | 應用明示 exporter／sampling／retention，Metrics 不放 owner labels |

CLI 專案產生器、Settings／Features／Cache 平台服務與其他 DB／Broker Adapter 目前尚未提供。請先依[安裝頁](../getting-started/installation.md)建立普通 Go 專案，再選擇需要的公開套件。

Go 使用 1.27.4；真實資料範例使用 PostgreSQL 18.7 與 NATS 2.15.3。初次學習核心功能時不需要安裝這兩個服務。
