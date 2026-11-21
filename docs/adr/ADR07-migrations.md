# ADR07 — Goose 與部署遷移

狀態：選型採用；正式遷移工具封裝待實作。對應 ENV03、DATA-002、A59。

唯一工具選 `github.com/pressly/goose/v3` v3.30.0，採 SQL migrations。版本記於 `deploy/toolchain.json`，實際導入時另保存 go.sum；目前 stdlib-only module 不為尚未使用的工具預裝大量依賴。

部署命令使用 Goose Provider 的 PostgreSQL session locker，於同一獨占連線持有 application/schema 的 advisory lock，按穩定 module DAG 順序執行全部 migration；不是每個模組自行競爭或 API 啟動時偷偷遷移。鎖鍵固定為 migration.LockKey（0x70616e676f6c696e），Goose 使用 WithLockID；版本表固定為 pangolin.goose_db_version。部署 Runner／Locker／Lock Port 已凍結於 persistence/migration/contracts.go；正式 Adapter 實作仍由 M3／A59 驗收。

schema 由 NOLOGIN owner 擁有；app 不擁有表、不關閉 RLS。預設向前修復；資料破壞／備份復原依獨立 runbook 與證據處理。啟用組織 profile 原子替換 permissive policies，不留下 OR 放寬。

依據：[Goose Provider／session locker](https://pressly.github.io/goose/documentation/provider/)、[v3.30.0](https://github.com/pressly/goose/releases/tag/v3.30.0)。
