# ADR13：M3 權威查驗、治理與可靠配送組裝

日期：2026-11-23。狀態：已採用；Runtime 驗收狀態由里程碑與 evidence 維護。產品版本維持 0.1.0，規範基線維持 v1.3。

## 問題與決定

M2 的 Scope／Organization capability 與 Bookstore 事件薄片不足以交付完整安全、治理與可靠配送。M3 依既有正式需求增加可重用 Adapter，不改變需求語意或穩定 ID。

- Scope Resolver 在 private context 保存中立 ScopeAdmitter；UOW 每個新根在 Begin 前查驗當前權威。Required／RequiresNew 保留父 admission 快照，後者不延長授權。Binding 是資料，Provider 的 Authorize 需核對當前可信 binding，不能把自行建立的 revision 當作權限。
- `tenancy` 提供當前 Tenant 資格 Port；`tenancy/postgres` 是本地一致投影。Organization 的 membership、父層、有效 owner、限制與 Tenant revision 由同一次 PostgreSQL 可見性快照取得。
- 治理 Management 保留獨立 Application contracts，透過 UOW、Idempotency 及 Effects Port 原子更新 Catalog／治理資料／Audit／Outbox。私有 schema 可在停止認領、Drain 後替換，穩定 owner Catalog 與事件不改 ID。
- Runtime `StartupChecks` 在 Provider seal 後、任何 Start 前執行，供部署組裝註冊 Schema Compatibility 與完整 Scope Inventory。受 RLS 過濾的普通應用查詢不能證明沒有其他 owner；Inventory 在可見性不足時拒絕，不能以空結果降級。
- Local Bus 與可靠 Publisher 透過 Resource wrapper 安裝根佇列。完成階段迭代 Local／flush、保存 Outbox 與必要 Audit；不改變 Manager 獨占一次 Commit 的責任。Inbox 持鎖與業務同交易，Outbox 短認領後在交易外配送。
- HTTP Pipeline 固定入口順序。提交後序列化錯誤或 panic 保留 Committed／operation ID；Unknown 仍優先，不宣告回滾。獨立 SecurityLog 可注入可信 IdentityReader，讓 Scope 拒絕前仍保留已驗證 Actor。

## Migration 與依賴

Goose 固定 v3.30.0。其 Up 先呼叫不使用 SessionLocker 的 HasPending，因此部署 Runner 用同一全域 session advisory lock 包住完整 Goose 操作，包括空庫 Schema／版本表初始化；解鎖由原獨占 session 執行。Goose 的 SQL 執行可用另一連線，部署 Pool 至少兩條連線。失去鎖確認的連線丟棄，不能回 Pool。

Goose 的最低依賴將 pgx 更新為 v5.12.0，其他解析版本與來源 checksum／license hash 固定於 deploy/dependencies.json。此清單保留完整 Go module graph，包含 Goose CLI／可選 Driver 的 module metadata；不表示框架新增 MySQL、SQLite 等正式 Adapter。新增 MIT-0、MPL-2.0 根授權檔的逐 module 精確核對，未知 module／版本／hash 仍拒絕。

## 驗證與相容性

公開 Surface 重新盤點，既有核心 Port 保留；新增組裝入口與 Runtime Options 欄位詳見 M3 變更說明。Go struct 的 positional literal 呼叫需改用具名欄位。測試使用真實 PostgreSQL／JetStream、具名 barrier、分程序 crash 與獨立連線對帳；不能用單元 mock 取代交易／Broker 證據。兩 Profile、Provider 替換與既有累積案例共同阻擋交付。

Ownerless 的停用由 optional Organization migration 對本地 Tenant qualification 更新安裝同交易 hook；鎖定 Catalog 再計算有效 owner，避免兩個 owner 同時撤權仍留 active。hook 只收緊授權，不阻擋 Tenant 撤權；Tenant 重新啟用不清除停用狀態。替代私有 schema 亦需提供相同 hook／一致更新契約，恢復只經專屬治理 Permission 與必要 Effects。
