# 測試與驗收索引

從 repository root 執行 Go 命令；完整 M2 累積 Gate 的服務與環境變數依 [M2 指南](../docs/guides/m2-development.md) 設定。

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
