# Packet P2 — M7 情境具名實作與契約註冊

Packet ID: P2
Objective: 為實作規範 §13「M7 即時通訊符合性矩陣」（RT-001–RT-009, A96–A104）的每個具名情境建立可執行測試並註冊契約。
Context: 分支 codex/signalr-realtime；既有未提交工作不得 reset／覆寫。modules/realtime 已有 hub.go／protocol.go／endpoint.go／contracts.go／notifications.go／jetstream/receiver.go 與 hub_test.go（3 個測試函式）。官方 client 整合測試 examples/realtime/client/integration.test.mjs 已 3/3 通過（兩節點、三 Profile）。Docker 服務已啟動：PG=26000、NATS=26001，env 見 examples/realtime/README.md。
Files / sources:
- design/panGOlin-framework_實作規範書.md（§13 M7 矩陣、§17 契約章）
- tests/contract/subtests.json（格式範本：groups→subtests，evidence 路徑 artifacts/{commit}/{profile}/…）
- tests/contract/m5.json（m7.json 格式範本：unit_tests/integration_tests/template_tests/pending/thin_slices）
- modules/realtime/**（測試放置處）、examples/realtime/**（必要時）
Ownership: 本包只新增測試、m7.json、subtests.json 的 A96–A104 groups、以及必要的測試輔助程式；不改 hub／protocol 行為、不改 spec。
Do:
- 依矩陣情境名稱（如 official-negotiate、token-binding、revocation barrier、commit-unknown、two-live-processes、slow-client 等）建立具名 Go 子測試，命名 TestA96RT…＋ subtests；低階 fixture 補官方 client 不會生成的負向輸入。
- 可用 hub_test.go 既有 helper；需要時在 modules/realtime/signalr 加 protocol 解析測試。
- 兩節點／真實服務情境若無法在單元測試覆蓋，標 pending 或映射到既有 integration.test.mjs（如 m5.json template_tests 的 string 型映射慣例）。
- 建立 tests/contract/m7.json；在 tests/contract/subtests.json 新增 groups A96–A104（requirement RT-00X，milestone M7，subtests 與實際測試函式逐一對應）。
- 對映的每個測試都要實際跑過：go test ./modules/realtime/... 或對應套件；輸出摘要寫入 results/p2.md。
Do not: 修改 production 行為、spec、既有測試語意；不得換模型；不得 git commit／reset。
Expected output: 具名子測試清單＋m7.json＋subtests.json 更新＋results/p2.md（命令與結果）。
Verification: go build ./...；go test ./modules/realtime/... ./internal/scaffold/...；若 quality 工具有子命令可跑契約盤點則執行。
