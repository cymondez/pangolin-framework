# M7 SignalR 即時通訊工作流

Goal: 完成 panGOlin 可選 SignalR 即時通訊模組（設計、實作、整合、測試、文件），交付同來源驗收證據，不更新任何既有 Verified。
Success criteria: 規範 RT-001–RT-009／A96–A104 有實作與可重現證據；官方 `@microsoft/signalr` client 互通；多租戶／企業／組織隔離與撤權；兩節點 JetStream 配送；Root／DI 組裝與 CLI Profile；race／vet／coverage；既有 D1–D5 與 Root／DI 回歸通過。
Current context: 分支 `codex/signalr-realtime`，工作樹含未提交的大量 realtime 工作（modules/realtime、events/nats fanout、examples/realtime 官方 client 測試、scaffold assets、spec §17 與 M7 矩陣）。`go build ./...` 通過；modules/realtime、events/nats、internal/scaffold 單元測試通過。
Constraints: 不 reset／覆寫既有修改；不把 SSE／WebSocket 包裝稱 SignalR；Mock 不作為 DB／Broker 語意證據；不更新 Verified。
Risks: Docker engine 權限可能受擋；長連線測試時序；coverage 門檻（§13.3）。
Approval required: 無（無破壞性操作；隔離 compose project 用畢依 AGENTS.md 清理）。
Work packets:（細節見 packets/）
- P1 現況盤點與整合測試執行（官方 client、兩程序、NATS／Postgres 真實服務）
- P2 M7 契約情境註冊（tests/contract/subtests.json + m7.json 映射）
- P3 race / vet / coverage / license
- P4 累積回歸與證據整併報告
Integration policy: 主代理審查與核對證據；程式與腳本產出由 subagent `glm-5.3-flash:cloud` 執行。
Verification: 先窄後寬：targeted tests → integration test → race/vet → 契約盤點。
Reusable artifacts: 本目錄 state.json / final-report.md。
