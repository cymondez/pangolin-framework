# SignalR 即時通訊範例

此 Root 應用在 loopback 執行，用本機示範憑證與 membership fixture，並透過真實 PostgreSQL、Outbox 和 NATS JetStream 發布。兩個程序各有自己的 NATS consumer。可選 `tenant-only`、`organization-enabled` 或 `replacement-contract`。示範身分僅可用於本機演練。

此範例同時啟動 Outbox worker（`jetstream.Route` 分流）與每節點 receiver，是『生成專案需另接 worker』的完整對照。服務輸出 `LISTEN http://127.0.0.1:8088/hub`。

從倉庫根目錄準備獨立服務；連接埠先確認空閒（本輪驗證使用 27121／27122）。以下為 PowerShell 指令。

```powershell
$env:PANGOLIN_PG_PORT = '27121'
$env:PANGOLIN_NATS_PORT = '27122'
docker compose -p pangolin-realtime-example -f deploy/compose/compose.yaml up -d --wait postgres nats
$env:PANGOLIN_REALTIME_ADMIN_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:27121/pangolin_m0?sslmode=disable'
$env:PANGOLIN_REALTIME_APP_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:27121/pangolin_m0?sslmode=disable'
$env:PANGOLIN_REALTIME_WORKER_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:27121/pangolin_m0?sslmode=disable'
$env:PANGOLIN_REALTIME_NATS_URL = 'nats://127.0.0.1:27122'
$env:PANGOLIN_REALTIME_DEMO_KEY = 'example-loopback-only-key'
go run ./examples/realtime --provision
go run ./examples/realtime --profile organization-enabled
```

`--provision` 必須在首次使用前執行：資料庫 migration／demo schema 未建立時，真實 PG 的 publish 測試與整合測試會失敗。同一 PowerShell session 保留以上環境變數再跑測試；整合測試以子程序繼承環境，缺 `PANGOLIN_REALTIME_ADMIN_DSN` 時 spawn 的 provision 會直接退出。

另一個終端機設定相同示範 key 後執行 client。

```powershell
$env:PANGOLIN_REALTIME_DEMO_KEY = 'example-loopback-only-key'
npm.cmd --prefix examples/realtime/client ci --ignore-scripts
node examples/realtime/client/demo.mjs
```

應看到 `Echo` 的結果與同一事件 ID 的推播。使用 `--listen 127.0.0.1:8089` 啟動第二個程序，可讓其中一個 client 連到 8089。測試套件會自動分開啟動兩個程序、檢查重複租戶內 ID、企業／組織會員、退訂、回滾及撤權重連。

```powershell
go build -o artifacts/realtime-demo.exe ./examples/realtime
$env:PANGOLIN_REALTIME_BINARY = (Resolve-Path artifacts/realtime-demo.exe).Path
npm.cmd --prefix examples/realtime/client test
```

整合測試會在每個 profile 結束時，以 `--cleanup-stream <串流名>` 刪除該次自建的 `RT_<randomhex>` 串流；JetStream 供應的 64MiB 配額因此會於每次執行後歸還。清理程序只需要 `PANGOLIN_REALTIME_NATS_URL`，不連資料庫，僅刪除命令列上指定的單一串流：先確認 StreamInfo subjects 恰為示範 subject `pangolin.realtime.<串流>`，不符即拒絕（絕不讀取、清空或刪除其他串流）；串流不存在視為成功（可重複執行），其餘錯誤保留原始原因。`--cleanup-stream` 與 `--provision` 互斥。harness 於單一 `t.after` 內先逐一關閉所有 client、再排放兩個 server 程序，最後才刪除串流——串流必須在 consumer 仍存在時讓 server 正常 Close，且該清理步驟在程式提前返回或任何斷言失敗時同樣會執行。

停止本次範例程序後清理此 Compose project（含 volumes）。

```powershell
docker compose -p pangolin-realtime-example -f deploy/compose/compose.yaml down --volumes
```

模組目前驗證到的範圍與未完成驗收見 [SignalR 現況報告](../../docs/evidence/SignalR-status.md)。
