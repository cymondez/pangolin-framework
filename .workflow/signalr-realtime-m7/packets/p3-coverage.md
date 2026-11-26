# Packet P3 — coverage 補足（§13 門檻）

Packet ID: P3
Objective: 將 M7 關鍵 production package 的 Go statement coverage 提升至規範 §13 門檻（關鍵 package ≥90%，原始計數）。
Context: 分支 codex/signalr-realtime，工作樹含既有未提交修改，不得 reset／覆寫。目前 Windows 本地 `go test -cover`：
- modules/realtime 72.5%
- modules/realtime/signalr 68.5%
- modules/realtime/jetstream 0%（115 行 production）
- events/nats 37.5%（broker.go 93 行；fanout.go 81 行為本次新增）
Production 檔案：modules/realtime/{contracts,hub,module,notifications}.go、signalr/{endpoint,protocol}.go、jetstream/receiver.go、events/nats/{broker,fanout}.go。
Ownership: 只新增／調整測試（*_test.go）與必要的測試 helper；不改 production 行為。測試放與套件並置。
Do:
- 為 modules/realtime、signalr、jetstream 補測至 ≥90%（以 `go test -cover` 原始計數判定）。優先覆蓋錯誤路徑：協定解析失敗、oversize、期限、佇列滿載、ACK 失敗、authority 故障、清理。
- events/nats：至少覆蓋 events/nats/fanout.go 的全部語句與主要錯誤路徑；broker.go 既有未覆蓋語句若屬既有交付（M6 已過 Gate），在回報中標記比例與未新增的理由，不阻擋本包。（目標：nats 套件總體 coverage 盡量拉高，至少 fanout.go 90%+。）
- 用 `go test -cover` 與（可行時）`-coverprofile` 記錄前後數字；不要為湊覆蓋率寫無斷言的測試。
- 跑 `go vet` 與 pinned Linux race（docker：`docker run --rm -v ${PWD}:/src -w /src -e CGO_ENABLED=1 golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea go test -race ./modules/realtime/... ./events/nats/...`）確認不因新測試破壞。
- 結果寫 .workflow/signalr-realtime-m7/results/p3.md：每 package 前後 coverage、race 結果、命令。
Do not: 修改 spec、改 production 行為、git commit/reset、更換模型、清理 docker compose。
Verification: go build ./...；go test -cover 上述套件；race PASS。
