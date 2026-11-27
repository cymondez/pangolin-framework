# ABP DX Remediation Final Report

## Result
Implemented the actionable items from `docs/reviews/2026-11-26-framework-abp-dx-review.md` on branch `codex/abp-dx-remediation` without committing.

## Delivered
- Wave 1: UOW error matching now preserves sentinel identity; foreign same-text errors no longer match. Added `CommandResult[T]` and `QueryResult[T]`, nil/typed-nil guards, zero-value-on-error semantics, and Bookstore migration.
- Wave 2: added typed `module.Resolve[T]`, `MustResolve[T]`, lifetime registration facades, and owner-safe `Runtime.OpenScope`. Updated examples, docs, and scaffold runtime templates.
- Wave 3: added buildable `module add --preset service`, valid dependency candidates, atomic staging/locks, and static/live doctor checks with bounded timeouts and no DSN leakage.
- Wave 4: added `upgrade plan`, `--dry-run`, `runtime`, and `verify`; three-way manifest diff, conflict refusal, profile/realtime switch guard, timestamped backup, rollback command, and readonly verification.
- Public API inventory and runtime bundle were regenerated; package docs and Docusaurus guidance were synchronized.

## Verification
- `go build ./...`: PASS.
- Production/internal `go test` excluding the pre-existing ignored artifacts reproduction tree: PASS.
- `go vet ./...`: PASS.
- `go run ./tools/check`: PASS.
- `python scripts/m0.py check`: PASS; 104 groups, 593 named subtests, 3 profiles, 49 API packages.
- `python scripts/template_bundle.py --check`: PASS.
- `examples/moduleapp`: `go test ./...` and `go vet ./...`: PASS.
- `docs/site` `npm run build`: PASS; 46 pages and 2445 internal links.
- `git diff --check`: PASS; only existing/generated LF conversion warnings.
- Independent final IntegrityReviewer verdict: PASS.

## Known Limits
- The unfiltered `go test ./...` includes pre-existing `artifacts/design-review-2026-11-24` reproduction failures. Those unrelated fixtures were not changed.
- A separate module `--preset crud` remains explicitly unsupported; the accepted Wave 3 exit criterion delivers the tested `service` preset.
- Live doctor uses bounded process/HTTP checks but was tested with fakes/httptest in this run; it was not connected to real Docker/PostgreSQL.
- The 0.1.0 preview still vendors the framework runtime bundle. Removing that bundle in favor of a tagged Go module source is a future released-version migration, not a safe pre-release in-place change.
- Remaining non-blocking test gaps: extra nil-able typed-nil variants and deterministic upgrade rename-rollback failure injection.

## Model and Branch
- Branch: `codex/abp-dx-remediation`
- Requested model: `MAI Code2 flash Extra high`
- Available subagent model used: `MAI Code 2 Flash (copilot)`; the subagent API did not expose an `Extra high` effort setting.
- No commit or push was made.
