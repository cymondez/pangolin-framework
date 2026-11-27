---
title: 0.1.0 功能相容性與支援邊界
description: 一覽 panGOlin 0.1.0 預覽版所有已驗證交付的能力、工具鏈版本與尚未涵蓋的明確邊界，協助團隊評估導入範圍。
---

本頁對應 **panGOlin Framework 0.1.0 預覽版**（尚無遠端正式發行 tag，導入方式採標準 Go module 搭配本機 `replace` 或透過 `pangolin` CLI 生成）。在將某項能力導入正式專案前，請先透過下表核對實際支援邊界。

---

## 1. 核心與平台能力矩陣

| 能力模組 | 0.1.0 已交付並經驗證 ✅ | 導入前務必注意的明確邊界 ⚠️ |
|---|---|---|
| **模組與生命週期** | Root Type 相依 DAG、穩定拓撲排序、三段 Configure、優雅停機 Drain、明示清單相容路徑 | 刻意不提供執行期動態熱插拔模組 |
| **依賴注入（DI）** | 建構子自動解析、`AddSingleton`／`AddTransient`／`AddOperation`、泛型 `Resolve[T]`、`rt.OpenScope`、跨模組 `Export` 與顯式 `Replace` 紀錄 | Singleton 不可捕獲 Operation 服務；建構子必須回傳具象型別 |
| **啟動設定（Config）** | `defaults < file < environment < cli` 合併與必填／URL 驗證 | 檔案與環境變數讀取由應用入口明確映射 |
| **工作單元（UOW）** | `application.CommandResult`／`QueryResult`、`Required`／`RequiresNew`、三種隔離級別、Deadline、`UOW_COMMIT_UNKNOWN` 與完成回呼 | 不提供非交易寫入、Savepoint、Suppress、跨資料庫原子提交或隱式重試 |
| **中立資料存取** | `persistence/database`（Resource／Executor／Dialect）支援 **PostgreSQL、MySQL、SQLite** | 能力差異於組裝期由 `database.Admit` 檢核；既有 Bookstore／治理／Audit／事件／Jobs 仍為 **PostgreSQL 專用 SQL**，不可只改 DSN 視為已移植 |
| **Application／HTTP** | 統一 Command／Query 政策、提交後才回應 2xx、`StreamOperation` 短交易、Problem Details | HTTP `Command` 拒絕加入外層 UOW；串流連線不可用單筆長交易包住 |
| **登入與身分驗證** | 協定中立 `identity.Verifier`／`Pipeline`、可擴充 `CredentialExtractor`、內建 RS256 JWT／JWKS Adapter | MFA／OIDC／Device Login 完整流程由獨立模組實作；Cookie 寫入須自備 CSRF 檢查 |
| **多租戶與授權** | 即時資格 Authority、新根 UOW revision 重驗、預設拒絕 + User Deny 優先、Resource Policy、PostgreSQL FORCE RLS | 權威故障預設拒絕；絕不信任請求 DTO 自報 Tenant／Scope |
| **組織治理（可選）** | `modules/organization`（PostgreSQL Authority、Management、HTTP Handler、Ownerless 復原、Bundle v1 Provider 替換） | 父層無子層資料聯集；已有非 Tenant 資料時不可直接降回 `tenant-only` |
| **稽核與冪等** | 同交易必要 Audit、獨立安全拒絕日誌、DB 唯一鍵冪等保留／重播與 `Unknown` 對帳 | 結果保護器（加密／編碼）與保留視窗須由部署明確配置 |
| **資料庫遷移** | Goose、PostgreSQL 全域 Session Advisory Lock、API 啟動唯讀相容性檢查、ScopeAware 前向升級 | 部署與 API 必須分開最小權限角色；API 啟動絕不自動執行 DDL |
| **事件與即時通訊** | 版本化 Registry、Local FIFO Bus、有界 PostgreSQL Outbox／Inbox、租約 fencing、NATS JetStream、可選 SignalR 即時通訊 | At-least-once 配送，外部副作用需穩定冪等鍵；`--realtime` 專案須另跑 Outbox Worker；其他 Broker Adapter 尚未提供 |
| **持久化背景工作** | 單次／延後 Job、版本 Registry、租約、取消、有界重試、同交易 Audit 重送 | At-least-once 執行（Handler 需冪等）；複雜 Cron 排程尚未提供 |
| **觀測與維運** | 明確注入 OpenTelemetry、遮罩 JSON Logs、`/livez`／`/readyz`、資料庫級有界背壓與 `QUEUE_PRESSURE` 告警 | Exporter／Sampling／Retention 由應用設定；Metrics 禁止放無界 Tenant／Org Labels |
| **設定、Features 與快取** | 5–7 層 Settings（含 JSON 陣列鍵）、Boolean／Quota／Enum + Ceiling、Permission 優先；Scoped Memory Cache | `MemoryProvider` 與 `Memory Cache` 僅存在單一程序；Redis Adapter 尚未提供 |
| **panGOlin CLI** | `new`（`tenant-only`／`organization`／`--realtime`）、`module add --preset service`、`migrate`、`doctor --live`、`generate openapi`、`upgrade` | `--preset crud` 明示不支援；預覽版內附同源 `framework/` bundle |

---

## 2. 實測驗證的工具鏈與外部相依版本

- **Go**：`1.27.4`
- **完整平台功能驗證環境**：PostgreSQL `18.7`、NATS `2.15.3`（JetStream FileStorage）
- **中立資料存取額外驗證引擎**：MySQL `8.4`、內嵌純 Go SQLite（`modernc.org/sqlite`，不需 CGO 或外部資料庫程序）
- **基礎教學需求**：零外部服務，僅需 Go 與 Git。

> **兩種 Bookstore 提醒**：CLI 生成的專案（HTTP 200、`id`/`title`）與框架內 `examples/bookstore`（HTTP 201、`id`/`title`/`version`）為兩套獨立示範，詳見[生成教學](../tutorials/generated-bookstore.md)與[Bookstore API 參考](bookstore-api.md)。
