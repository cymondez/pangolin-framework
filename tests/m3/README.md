# M3 真實服務與契約驗收

正式 M3 映射在 [m3.json](../contract/m3.json)，40 項需求、185 個具名情境；執行前讀 [開發指南](../../docs/guides/m3-development.md)。測試缺配置直接失敗，不使用 skip 取代驗收。

各套件使用專用 PostgreSQL 中的獨立資料庫，透過 pgx／Goose 與非特權 app／worker role 執行；結束後只刪除該測試建立的資料庫。`fixture_test.go` 提供共用 Port 與原始組裝；組織／事件／冪等各自的 fixture 不共用業務狀態。驗收結束由 host 清除本次 Compose volumes／network／專用映像，保留 workspace logs。

- `security_test.go`、`tenant_test.go`、`authority_audit_test.go`：Tenant／owner RLS、複合 FK、Pool 不污染、權威 revision、Audit 原子性與啟動降級拒絕。
- `organization_test.go`、`replacement_test.go`：治理／角色／撤權／生命週期，獨立私有 schema 匯入及既有事件重新授權重播；管理 HTTP unit 位於模組自身目錄。
- `events_test.go`、`broker_test.go`：完整 Envelope、Lease fencing、持鎖 Inbox、分程序 crash、外部穩定冪等鍵與真實 JetStream file store。
- `idempotency_test.go`、`migration_test.go`：唯一約束等待 barrier、回滾釋放、同鍵成功復原／Unknown 對帳、版本相容性、全域鎖與前向升級中斷復原。

Broker 重啟案例透過 `PANGOLIN_M3_BROKER_RESTART_DIR` 的 file barrier 交給 host `scripts/m3_fixture.py`，僅重啟已核對 labels 的本次專用 NATS。Linux runner 不取得 Docker socket。NATS ACL 的部署責任與可信 producer verifier 必須明示；測試不宣稱已驗證外部生產環境 ACL 配置。

完整 Gate 使用 `scripts/m3.py verify`，累積 M0/M1/M2、分程序 E2E、90% 整體／關鍵 package、sqlc、依賴／license。Linux 加 race；雙平台的 source hash、commit、命令／profile／artifact hash 一致才可由 gate 判 Verified。單獨執行某個 Test 的成功不等於完成 M3。
