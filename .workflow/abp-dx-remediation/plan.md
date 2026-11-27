# ABP DX Review Remediation

Goal: 依 `docs/reviews/2026-11-26-framework-abp-dx-review.md` 的四個 Wave 修正框架 correctness、公開 API ergonomics、CLI generation/diagnostics 與 upgrade workflow。

Success criteria:
- UOW sentinel 比對只接受原始錯誤身分，不以相同字串誤分類。
- Application 提供 `CommandResult[T]`／`QueryResult[T]`，Bookstore 與 scaffold 使用共用入口。
- Module 提供 typed resolve、lifetime registration façade 與 Runtime operation scope；不降低 visibility／owner 限制。
- CLI 支援可建置的 service preset、分層 `doctor --live` 與安全的 upgrade plan/dry-run/verify。
- examples、template、README／migration docs 使用相同推薦 API。
- Focused tests、`go test ./...`、`go vet ./...` 與必要 CLI temp-project smoke test 通過；完整 release Gate 未在本輪自動宣稱 Verified。

Current context:
- Branch: `codex/abp-dx-remediation`
- Review baseline: `develop` at `3db4958`
- Pre-existing untracked input: `docs/reviews/`
- No destructive Git, deployment, credential or production-data work.

Constraints:
- 每個 implementation packet 只修改不重疊的套件／檔案範圍。
- 不覆寫 review 以外的既有修改；CLI upgrade 不得自動覆寫使用者修改。
- 不改 UOW、Scope、RLS、Outbox、visibility 的安全與交易不變量。
- Agent 使用 API 實際提供的 `MAI Code 2 Flash (copilot)`；subagent API 未提供 `Extra high` effort 參數。主控負責整合、衝突解決與結果驗收。

Risks and rollback:
- 風險低至中：高階 façade 為加法式；CLI generator/upgrade 會改使用者流程。
- 每個 Wave 以獨立、可重跑 focused test 驗證；若失敗則停住該 Wave 修復，不跨 Wave 疊加。
- 本輪不 commit、不 push；回滾策略為不納入失敗 diff 或由主控精確撤銷本輪檔案變更。

Work packets:
1. P0 Discovery: 盤點控制點、既有 API、測試、generated templates、contract checks。
2. P1 Wave 1: UOW sentinel identity、application result helpers、Bookstore migration、focused tests。
3. P2 Wave 2: typed resolve/registration、Runtime/OpenScope façade、examples/docs/scaffold integration。
4. P3 Wave 3: service preset、invalid dependency diagnostics、doctor static/live layers。
5. P4 Wave 4: upgrade plan/dry-run/runtime/verify、manifest diff、conflict policy、docs。
6. P5 Integration QA: API inventory、focused/full tests、vet、CLI smoke and integrity review。

Integration policy:
- 接受有測試證據、維持既有 invariants、符合既有 Go style 的變更。
- 拒絕以字串 heuristic、silent overwrite、raw engine exposure、弱化型別／安全契約的捷徑。
- 重疊檔案由主控序列整合；worker 不可回復其他 agent 修改。

Verification:
1. P1: `go test ./persistence/uow ./application ./examples/bookstore/application ./transport/http`
2. P2: `go test ./kernel/module ./kernel/di ./kernel/app ./internal/scaffold ./examples/moduleapp/...`
3. P3/P4: `go test ./internal/scaffold ./cmd/pangolin`; clean temp project generation + `go test ./...`; upgrade dry-run no-write assertion.
4. Integration: `go test ./...`, `go vet ./...`, API inventory and review links/checksum checks.

Reusable artifact: 本目錄保存分工、狀態與最終驗收報告；不保存 secrets、完整 log 或 session transcript。
