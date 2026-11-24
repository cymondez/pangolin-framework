# 測試與驗收索引

從 repository root 執行 Go 命令；目前完整 M4 累積 Gate 的服務與環境變數依 [M4 指南](../docs/guides/m4-development.md) 設定。

[M4 驗收](m4/README.md) 保存持久化 Jobs、Broker 容量恢復、10,000 筆負載、活躍停機與 OpenTelemetry 案例；目前各項是否 Verified 仍由同版 Gate 決定。

| 位置／入口 | 責任 |
|---|---|
| 各 package 的 `_test.go`；`go test ./...` | 同套件單元測試與公開契約測試，不需要外部服務 |
| `m0/test_gate.py` | M0 契約計畫、Profile／API／同版 evidence 的工程反例 |
| [M2 驗收](m2/README.md) | UOW／Scope／HTTP／串流、版本衝突與真實 COMMIT frame 故障 |
| `m1/test_gate.py` | M1 失敗／skip／依賴／coverage／架構反向依賴防護 |
| `m1/flow_test.go`；`go test -tags=integration ./tests/m1` | 真實 PostgreSQL／NATS 交易、RLS、事件落地與對帳；缺設定直接失敗 |
| [M1 分程序 E2E](../scripts/m1_e2e.py) | 真實 API／Worker、JWT、Broker ACK 後崩潰重送與插樁 coverage，由完整 Gate 呼叫 |
| `contract/` | A01–A95／Profile 計畫、凍結 API 與 M1 執行案例／文件索引 |
| `fixtures/` | 固定 seed 的資料與故障情境計畫 |
| `coverage/` | 逐檔排除清單；手寫 production code 仍進分母 |

共用 IdP／Profile fixture 位於 [internal/testkit](../internal/testkit/README.md)。Bookstore 專用 crash-worker／m1-query 在 [範例 testkit](../examples/bookstore/internal/testkit/README.md)，不散放在產品 cmd，也不供 production 使用。

執行結果只寫入 artifacts/<commit>，不放回 tests。每次搬移都回歸先前已交付案例；舊來源版本的驗收紀錄與 hash 留在 [docs/evidence](../docs/evidence/M1.md)。完整操作入口見 [工具索引](../tools/README.md)。

[M3](m3/README.md) 新增 185 個具名情境、Tenant／Scope 安全、治理／替換、可靠事件與冪等／遷移；正式入口為 scripts/m3.py。測試計畫與 pass evidence 分開，缺案例／skip／同版 hash 不一致均不能通過 Gate。

M5 平台／CLI 與兩 Profile 生成物：正式索引 [m5.json](contract/m5.json)、[測試說明](m5/README.md)。平台單元測試與套件並置，組織 ancestry／Audit 真實回歸在 m3/platform_test.go；完整入口 scripts/m5.py。
