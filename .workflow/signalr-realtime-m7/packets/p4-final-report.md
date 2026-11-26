# Packet P4 — 最終報告與狀態更新

Packet ID: P4
Objective: 整併 P1–P3 證據，寫出 .workflow/signalr-realtime-m7/final-report.md 並更新 state.json。
Ownership: 只寫 .workflow/ 下的報告與狀態檔；不改程式、spec、測試。
Evidence inputs（已由主代理核對，直接引用，不要重跑重量驗證；如需補輕量確認可用 -run 精確跑）:
- build：`go build ./...` PASS
- 單元回歸：`go test ./...` —— production 套件全綠（含 modules/realtime、signalr、jetstream、events/nats、internal/scaffold、kernel/*、rootmodules）；唯一失敗在 `artifacts/design-review-2026-11-24`（歷史重現目錄 6 項：TestLocalSwallowedNestedBusyCompletesRoot、TestLocalPropagatedNestedBusyFailsRoot、TestDIResolveHoldsContainerMutexAcrossFactory、TestDIOpenScopeWaitsForGlobalMutexHeldByFactory、TestDICloseHoldsContainerMutexAcrossCleanup、TestJWTRequestsJWKSForEveryValidShapeBeforeSignatureRejection）。該目錄為 2026-11-24 設計審查輪的凍結重現，kernel/di、persistence/uow、security/authn 在本次工作樹未變（git diff HEAD --stat 這些路徑為空），失敗為 branch 基線 c868bfd 合併後既有狀態，非 M7 引入，已標記為需另行確認的限制。
- 官方 client 整合測試（examples/realtime/client，@microsoft/signalr 10.0.0，兩個獨立 server process，docker PG=26000/NATS=26001）：tenant-only / organization-enabled / replacement-contract 3/3 PASS（最終形態重跑；中途失敗僅為 NATS RT_* 串流配額殘留，purge 後復通）。
- race：Windows 主機缺 gcc 不執行；pinned `golang:1.27.4-bookworm`（docker）`go test -race ./modules/realtime/... ./events/nats/...` PASS；`go vet` PASS。
- coverage（go test -cover 原始計數）：modules/realtime 72.5→92.7%、signalr 68.5→93.6%、jetstream 0→93.2%、events/nats 37.5→92.5%；~2 語句不可達殘餘記於 results/p3.md。
- 契約：tests/contract/m7.json（35 unit + 17 template 映射、9 項 pending 附理由）、subtests.json 增加 A96–A104 groups 61 個情境（diff 純新增）；internal/quality/contract.go 校驗 RT-001..009 ↔ A96–A104。
- 環境隔離：compose project pangolin-realtime-example 使用 26000/26001；殘留的其他容器 pangolin-signalr-1a215bc-*（25342/24222）為先前 session 存留，未動。
Do:
- 寫 final-report.md，分四節：完成的功能（含檔案/路徑）、實際通過的驗證（命令＋結果）、未支援／未執行、尚未通過的正式發布 Gate（M7 Gate 未執行、Linux 全量 race 與 §13.3 分平台 coverage 未收集、未更新任何 Verified、artifacts 歷史重現 6 項失敗待確認）。
- 更新 ../state.json：packets 全部 completed，verification 各項填實際結果。
Do not: git commit/reset；更換模型；重跑 docker compose 建置。
Expected output: final-report.md + state.json。
