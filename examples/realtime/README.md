# SignalR 即時通訊範例

此 Root 應用在 loopback 執行，用本機示範憑證與 membership fixture，並透過真實 PostgreSQL、Outbox 和 NATS JetStream 發布。兩個程序各有自己的 NATS consumer。可選 `tenant-only`、`organization-enabled` 或 `replacement-contract`。示範身分僅可用於本機演練。

從倉庫根目錄準備有已知空閒連接埠的獨立服務。以下為 PowerShell 指令。

```powershell
$env:PANGOLIN_PG_PORT = '25342'
$env:PANGOLIN_NATS_PORT = '24222'
docker compose -p pangolin-realtime-example -f deploy/compose/compose.yaml up -d --wait postgres nats
$env:PANGOLIN_REALTIME_ADMIN_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:25342/pangolin_m0?sslmode=disable'
$env:PANGOLIN_REALTIME_APP_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:25342/pangolin_m0?sslmode=disable'
$env:PANGOLIN_REALTIME_WORKER_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:25342/pangolin_m0?sslmode=disable'
$env:PANGOLIN_REALTIME_NATS_URL = 'nats://127.0.0.1:24222'
$env:PANGOLIN_REALTIME_DEMO_KEY = 'example-loopback-only-key'
go run ./examples/realtime --provision
go run ./examples/realtime --profile organization-enabled
```

服務輸出 `LISTEN http://127.0.0.1:8088/hub`。另一個終端機設定相同示範 key 後執行 client。

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

停止本次範例程序後清理此 Compose project。

```powershell
docker compose -p pangolin-realtime-example -f deploy/compose/compose.yaml down --volumes
```
