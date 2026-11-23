# M4 Runtime Acceptance

覆蓋 A63 的 Broker 離線／容量／重試上限／dead replay／恢復與兩 Tenant 對帳，A64 的持久化 Jobs、取消、token fencing、原子 Audit、owner 竄改及 Handler 冪等，A66 的完整 OpenTelemetry 關聯。Health／redaction 的無服務案例與 package 並置。18 個正式情境由 [m4.json](../contract/m4.json) 對應，工程 Gate 不代替 Runtime。

每個套件測試建立自身 `m4_*` DB，結束即清除；使用固定版本 PostgreSQL／NATS 與最小權限 app／worker role。所有服務變數沿用 `PANGOLIN_M1_*` 以供累積回歸。NATS stop／start／restart 透過經 project ownership 核對的 host fixture；不要直接執行整套測試而不啟動 fixture。

`TestCapacityLoad` 以 64 個 producer 接收 10,000 筆持久化工作，再以固定 4 個 Worker 排空，保存 p95／p99、pool／heap／pending／完成數曲線。`TestActiveHTTPWorkerDrain` 使用 barrier 驗證正常及逾時停機、活躍 HTTP 和 Job、Handler 退出後才交還 lease、DB 最後關閉。A79 的 scope／撤權／替換契約隨 M3 累積回歸。

完整命令與隔離環境清理見 [M4 開發指南](../../docs/guides/m4-development.md)。
