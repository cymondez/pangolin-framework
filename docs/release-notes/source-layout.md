# M1 後續：目錄歸屬整理

2026-11-21。採 [ADR10](../adr/ADR10-source-ownership.md)：不增加 src，framework 的共用公開套件與 module path 不變；Bookstore 專用程式集中於自己的範例。產品行為與需求 ID 不變。

| 原位置／命令 | 新位置／命令 |
|---|---|
| `go run ./cmd/bookstore-api` | `go run ./examples/bookstore/cmd/api` |
| `go run ./cmd/bookstore-worker` | `go run ./examples/bookstore/cmd/worker` |
| `github.com/cymondez/pangolin-framework/transport/http` | `github.com/cymondez/pangolin-framework/examples/bookstore/transport/http` |
| `internal/testkit/cmd/crash-worker` | `examples/bookstore/internal/testkit/cmd/crash-worker` |
| `internal/testkit/cmd/m1-query` | `examples/bookstore/internal/testkit/cmd/m1-query` |

採用過 Bookstore Handler 的程式需更新 import；型別、建構與 HTTP 行為不變。舊 Bookstore main 的路徑不保留轉接命令。E2E 會從新路徑建置，輸出 binary／log 名稱仍為 bookstore-api／bookstore-worker，對帳流程與原始 evidence 格式維持一致。

同套件 `_test.go` 與程式碼並置；跨套件驗收、共用 fixture、操作腳本與 Go 命令分別在 tests、internal/testkit、scripts、tools，新增總覽說明入口。原 M0／M1 驗收紀錄及其來源 hash 不改寫；本次重跑結果另存，不能以舊位置的 profile 代表新工作樹。
