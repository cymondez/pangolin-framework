# M6 bounded discovery results (Flash, read-only except this file)

Date: 2026-11-24. Repo: branch `codex/m6-release-acceptance`, HEAD `62adaa5` (merge of M5 corrections into develop), only untracked `.workflow/m6/`. Product version `0.1.0` (`VERSION`; CLI `internal/scaffold/cli.go:28`).

## 1. Sources read

- `AGENTS.md`; `.workflow/m6/{plan.md, packets/*}`.
- Spec: §13 (`design/panGOlin-framework_實作規範書.md:1359-1522`), §14 (`:1523-1557`), §15 (`:1558-1571`), §16 REL-003/REL-005/ORG-014 (`:1572-1701`).
- Milestone: M6 node `design/panGOlin-framework_里程碑規劃書.md:236-250` (delivers REL-003, REL-005, ORG-014), WP18 row `:404`, delivery map §5 `:275-378` (M6 rows only A70/A72/A95, Planned), §8 DoD `:438-455`.
- Gate chain: `scripts/check.ps1` 3 lines → `python scripts/m5.py verify --project <p> [--race]`; `scripts/check.sh` → `python3 scripts/m5.py verify`; `m5.py` adds M5 mapping/template validation then delegates to `m1.verify` (unit→integration→E2E→templates→CLI coverage→merge→race→sqlc→coverage count) and `m5.py gate` compares windows/linux reports; `scripts/m0.py check` = contract inventory + API freeze + imports + dependency lock; `go run ./tools/check` = `-coverage`/`-release` modes.
- CI: `.github/workflows/ci.yml` — "M5 cumulative gates" (linux full gate with compose `deploy/compose/m1.yaml`+`m5-databases.yaml`; windows job = build/vet/unit JSON only; installs sqlc v1.31.1 into `.cache/bin`).
- Profile manifests: `deploy/profiles/local.json` (M0 config baseline; `capacity_fixture.results`="not-measured", `:45-53`); `tests/contract/profiles/{tenant-only,organization-enabled,replacement-contract}.json`; coverage exclusions `tests/coverage/exclusions.json` (11 files: 5 testkits, 6 sqlc-generated).

## 2. 421 subtest ID audit — mapping is exact

- `tests/contract/subtests.json`: 95 groups, **421 unique IDs**, all match `^A\d{2}/[a-z0-9-]+$`, all 95 cases represented. Milestone split: M1 47 / M2 133 / M3 185 / M4 18 / M5 25 / M6 13.
- Generated deterministically from the spec acceptance table (row sha256 locked) by `scripts/contract_matrix.py:112-135` (`generate()`); per-case subtest names come from `SCENARIOS` (`scripts/contract_matrix.py:13-109`; A70/A72/A95 at `:83-85`, `:95`).
- Uniqueness/coverage enforced at 4 layers: `m0.check_profiles` duplicate check (`scripts/m0.py:69-93`); `quality.ValidateContract` duplicate-requirement/duplicate-delivery checks via regexes (`internal/quality/contract.go:26-66`); per-milestone map union+count validation (`scripts/m5.py:24-29`, `scripts/m4.py:10-14`, `scripts/m1.py:64-67`); Python regression `tests/m0/test_*.py::test_all_named_plans_and_fixture_references` asserts `{groups:95, named_subtests:421, profiles:3, fixture_files:4}`.
- Cross-check: each `tests/contract/m{1..5}.json` maps **exactly** its milestone's IDs (union==expected, no blank/pending). M5 example: 22 unit + 2 integration + 0 python + 1 template = 25.
- Profile applicability: tenant-only 343 required / 78 N-A (§13.4 A83–A93, A76, parts of A74/A77/A79/A80/A81/A94); organization-enabled and replacement-contract 421/0 required. Required negatives that may never go N/A: A75/A92/A95 and `A74/missing-required-provider|duplicate-kind|unsupported-kind-no-fallback`, `A77/missing-scope|pool-reuse`, `A79/missing-org-scope|unavailable-provider` (`scripts/m0.py:89-91`).
- **Only unmapped group: the 13 M6 subtests** — A70×4 (`missing-release-item`, `unknown-requirement`, `duplicate-delivery`, `template-runtime-version`), A72×5 (`clean-rebuild`, `forward-upgrade`, `backup-restore`, `load-baseline`, `security-review`), A95×4 (`organization-contract-fails`, `applicable-case-skipped`, `critical-package-below-threshold`, `tenant-only-build-independent`). All `status:"planned"`, evidence template `artifacts/{commit}/{profile}/Axx/<subtest>/`.

## 3. A70/A72/A95 → genuine negative test design

### 3.1 Current fail-closed placeholders (what exists today)

- `internal/quality/contract.go:85-87` — `ValidateContract(..., release=true)` hard-errors `"release blocked: all 95 runtime case groups await executable tests and same-commit evidence"`; wired via `-release` flag at `tools/check/main.go:22,30`.
- `scripts/m0.py:228` appends `go run ./tools/check -release` in `m0 verify`; `:235` expects exit 1; `:268,285` `validate_report` requires one failed `-release` command in every M0-style evidence (cumulative gates keep proving the fail-closed guard).
- `tools/check/main_test.go:22` asserts the CLI rejects `-release`; documented at `tools/check/README.md:5`.
- Skip/fail fail-close in the real runner: `m1.validate_tests` (`scripts/m1.py:28-42`) rejects any `Action=="fail"` or `Action=="skip" and Test!=""`; unit negatives at `tests/m1/test_*.py::test_A69_failure_skip_and_missing_case` and `tests/m5/test_gate.py::test_required_skip_fails`.
- Coverage-collector negatives exist: `tools/check/main_test.go:44-105` (89.99 / exact-threshold / aggregate-masks-critical / untested-package / duplicate-block-union / missing-profile fixtures) against `internal/quality/coverage.go` (`Counts.Pass()` raw-count integer compare, no rounding).
- Version-binding negatives exist: `tests/m0::test_product_version_rejects_milestone_and_module_drift` mutates VERSION/scaffold const/bookstore bootstrap/modules fixture/site package-lock one at a time (`scripts/product_version.py:16-38`); template-bundle same-source check `scripts/template_bundle.py bundle()` ↔ `internal/scaffold/assets/**` (`tests/m5::test_runtime_bundle_same_source`).

### 3.2 Genuine negatives = same validator, mutated committed fixtures

**A70 (REL-003).** Replace `contract.go:85` with a deterministic release-manifest validation (new function, e.g., `quality.ValidateRelease`) that checks the committed release manifest against: 95 unique requirements/deliveries (reuse `deliveryRE`/`caseRE`), 421 subtests completeness (subtests.json), version binding (VERSION==scaffold const==template bundle==runtime-manifest hashes: `internal/scaffold/assets/common/runtime-manifest.json.tmpl` vs `template_bundle.bundle()`), sqlc/Go/toolchain pins (`deploy/toolchain.json`), and evidence refs (per-platform `artifacts/<commit>/m6/<platform>/report.json` + digests). The 4 negatives run the same function on committed mutated fixtures:
- `A70/missing-release-item`: delete one required block (e.g., migration-notes ref) → error must name the missing item.
- `A70/unknown-requirement`: inject unmapped requirement (REL-999 / 96th ID / M6 claiming already-claimed REL-001) → unknown/duplicate delivery error (duplicate-delivery logic reused, `contract.go:63-66`).
- `A70/duplicate-delivery`: two milestone rows deliver one requirement → existing duplicate check fires (`contract.go:63-66`).
- `A70/template-runtime-version`: drift the runtime-manifest hash entry or template VERSION → mismatch detected (negative analog of `template_bundle --check` + `m5_templates.py` runtime-manifest verification `scripts/m5_templates.py:89-92`).
The positive run validates the real committed manifest with real evidence; no hand-edited status flags (existing rule: `contract.go:81-82` rejects any status other than "planned" in cases.json — keep that). Version-check analog negatives already exist (`scripts/product_version.py:9-38`).

**A72 (REL-005).** Positive drills whose assertions make any missing condition fail:
- `clean-rebuild`: reuse `m0.py verify --environment` (compose build + `scripts/smoke.py`, 9+ environment checks) on an isolated project from clean runner; assert image/tool versions + hash-consistency (`template_bundle.py --check`).
- `forward-upgrade`: reuse `tests/m3/migration_test.go:161 TestA93ForwardUpgrade` (tenant→org backfill + atomic policy switch + fail-drill variants) and `:48 TestA59Deployment`; release drill wraps the full generated profile switch (M5 `m5_templates.py` flow + `cmd/migrate --seed` idempotency + `migrate status`).
- `backup-restore`: **new** — pg_dump/restore + post-restore reconciliation of business/outbox/inbox/idempotency/audit counts vs pre-dump counts; produce measured RPO/RTO. Nothing exists today; M4 evidence explicitly defers it (`docs/evidence/M4.md:27`).
- `load-baseline`: extend `tests/m4/load_test.go:112-140 TestCapacityLoad` (already writes `operations.json`: p95_us/p99_us/reconciled/samples/pool/heap) with hardware, data-set, pool config, method and error margins; replace `deploy/profiles/local.json` capacity_fixture `results:"not-measured"` with measured values or fail.
- `security-review`: **new** — §14.1 record listing attack surface / abuse path / control / test / residual risk per threat class, mapped to verified cases (A08, A12, A13, A56, A57, A58, A62, A77, A79, A92...). No threat-model doc exists today (`rg` across docs finds only runbook RPO/RTO notes, `docs/runbooks/operations.md:19`).

**A95 (ORG-014).** Four subtests; all four already have fail-closed validators — M6 must produce same-commit drill evidence that the real M6 runner blocks each condition:
- `organization-contract-fails`: run the m6 report-validation path (`m1.validate_tests`/`validate_report` chain) against a fixture report whose org integration case fails → non-zero; drill mirrors `tests/m5/test_gate.py` style.
- `applicable-case-skipped`: same validator path fails on skip; plus profile level `m0.check_profiles` N/A denial (`m0.py:89,91`; regression-covered `tests/m0::test_missing_duplicate_false_pass_and_na_are_rejected` which also asserts `A95/tenant-only-build-independent` cannot be N/A).
- `critical-package-below-threshold`: `tools/check/main.go:74-83` critical list (spec §13.3 superset — adds persistence/database, -mysql, -sqlite per ADR14); fixture negatives already pass in unit tests (`tools/check/main_test.go:44-105`, A68 subtests Verified in M1); release drill runs checkCoverage on a mutated profile at the runner level (`m1.verify` coverage step) and fails.
- `tenant-only-build-independent`: existing guards — `m0.check_imports` (`m0.py:97-118`), tenant-only graph capture guard (`m0.py:188-191`, regression-tested `tests/m1::test_framework_never_imports_examples`), `m5_templates.py:88` tenant template `go list -deps` check, generated-template contract checks `complete-control-plane`/`profile-downgrade-blocked` (`scripts/m5.py:46-47`). Release evidence = same-commit proof in both profiles.

## 4. Exact release artifacts (WP18 → §8) and gaps

| Artifact | Status |
|---|---|
| `tests/contract/m6.json` — 13 subtest→test-name map, regression lists, module_docs, thin_slices | **missing** (required by m6.validate_mapping; blanks/pending fail closed) |
| `scripts/m6.py` — verify+gate mirroring `scripts/m5.py`; adds release-manifest, drill-evidence and A95-negative validation; calls `m1.verify(..., increment=m6mapping)` | **missing** |
| Release manifest file (baseline v1.3, source commit, CLI/Runtime/Template version 0.1.0, change/migration notes, dependency/toolchain pins) | **missing** (milestone §8 last row; spec §15) |
| Release Notes / Migration Plan (§15: added/changed/removed/breaking, min Go/PostgreSQL) | **missing** |
| A72 drill scripts + evidence JSON (upgrade/restore/load/security under `artifacts/<commit>/m6/...`) | **missing** (partial reuse available, see §3.2/§5) |
| Security review record | **missing** |
| `artifacts/<commit>/m6/{windows-amd64,linux-amd64}/report.json`, `gate.json`, `docs/evidence/m6-<sha12>.json`, `docs/evidence/M6.md` | **missing** (bind pattern like `docs/evidence/m5-0d811a21.json`: report digests, gate_sha256, runner versions, regression lists) |
| `scripts/check.ps1` / `check.sh` → m6.py | stale (still call m5.py) |
| CI workflow | `.github/workflows/ci.yml:1` named "M5 cumulative gates"; compose/env reusable; artifact name `m5-linux-amd64` |
| E2E namespace | **must change**: `scripts/m1_e2e.py:16` regex `pangolin-m[12345][-a-z0-9]*` rejects m6; `tests/m5/test_gate.py::test_e2e_namespace_guard` asserts `pangolin-m6-dev` rejected |
| `-release` flip | `internal/quality/contract.go:85-87`, `scripts/m0.py:228,235,268,285`, `tools/check/main_test.go:22` |
| Milestone doc §5 three M6 rows → Verified + M6 guide `docs/guides/m6-development.md` + README pointers + runbook drill sections; Docusaurus content after acceptance | pending |

Coverage headroom (last same-commit M5 evidence, `artifacts/0d811a21.../m5/*/coverage.json`): overall 93.14% linux / 93.16% windows (5,392/5,789 vs 5,393/5,789 statements); **zero critical §13.3 packages below 90%** (`tools/check/main.go:74-83` critical list covers kernel, persistence/{uow,postgres,database,mysql,sqlite}, security, tenancy, events, platform/audit, modules/organization). Sub-90% packages are all outside the critical list (`cmd/pangolin` 5/6, `examples/bookstore/{application,bootstrap,cmd/api,cmd/worker,domain,infrastructure}`, `internal/scaffold` 321/378). Watch: code added for M6 (if Go) enters the denominator — `-coverpkg` takes the full `go list ./...` minus testkit/generated-infra paths (`scripts/m1.py:173`); Python runner code does not.

## 5. Cheapest reusable test entry points

1. **No services needed** — `python scripts/m0.py check` (contract inventory incl. profiles + API freeze + imports + tenant-only graph + license/deps); `go test ./tools/check`; `python -m unittest discover -s tests/mN -p test_*.py` (m0..m5 Python regressions; m6 mirrors `tests/m5/test_gate.py`).
2. **Negative-fixture harnesses** — temp-dir mutation style: `tools/check/main_test.go` (coverage/release), `tests/m0/test_*.py` (profiles/versions), `tests/m5/test_gate.py` (mapping/report/OpenAPI). These are the direct templates for A70/A95 negative fixtures.
3. **Real-service suites** — `scripts/m{5→6}.py verify --project pangolin-m6-dev --race` after fixing the namespace regex; requires `PANGOLIN_M1_{ADMIN,APP,WORKER}_DSN`, `PANGOLIN_M1_NATS_URL`, `PANGOLIN_M1_ISSUER` (values from `docs/guides/m5-development.md`; CI uses the compose ports). Broker-restart fault fixture `m3_fixture.broker_restart`; backend suite `tests/database` auto-required for M5+ (`scripts/m1.py:44-49 database_packages`).
4. **Drill reuse** — A72/forward-upgrade: `TestA93ForwardUpgrade`/`TestA59Deployment`; load: `TestCapacityLoad` + `PANGOLIN_M4_METRICS_DIR` (set by `m1.verify` for M4+, `scripts/m1.py:140`); clean rebuild: `m0.py verify --environment`; sqlc: `.cache/bin/sqlc` version-enforced `scripts/m1.py:197-198`.
5. **Evidence merge** — `m1.merge_profiles` (`scripts/m1.py:80-96`) unions blocks across unit/integration/e2e/cli profiles; same-source/build identity then verified by `report.source_sha256` equality in `m1.gate`/`m5.gate`.

## 6. Local environment inventory (read-only probes)

- Go 1.27.4 windows/amd64 == deploy/toolchain.json `go` key; `GOTOOLCHAIN=local` enforced by `m0.go_environment`.
- Python 3.13.5.
- Cached binaries: `.cache/bin/sqlc.exe` (pinned v1.31.1, 74,871,296 bytes; not executed in this packet — CI re-installs pinned sqlc; `m1.verify` executes it during gate), `.cache/bin/pangolin.exe` (16,664,064 bytes, 2026-11-23).
- Cached evidence: `artifacts/<sha>/m{0..5}/...` for 13+ commits — include the two M5-verified commits (`88f32eb6`, `0d811a21`); `artifacts/m{3,4,5}-broker-barrier/` fault-fixtures; `docs/evidence/*.json` bind report/gate digests per platform.
- Docker Desktop installed; engine connection not exercised in this read-only packet (no services started).

## 7. Risks / notes for gate implementation

- Flipping `-release` (A70) retroactively changes M0 semantics: `m0.py:228,235,268,285` and `tests/m0` (`test_same_commit_logs_and_required_commands_are_enforced` builds a report with `-release` expected exit 1) are re-run by every cumulative gate — update validator, CLI tests, and M0 Python regressions in the same change, keeping fail-closed for missing evidence.
- `m6.json` must follow the mapping invariants: every `m{N}.validate_mapping` pattern rejects missing/blank/pending entries; `named_subtests` in the M6 gate should be `sum(len(mapping[k]))` like `m5.py gate` (`scripts/m5.py:37-38`) rather than a literal.
- Same-commit discipline: `m1.source_hash` covers tracked+untracked files (`scripts/m1.py:19-26`) — the `.workflow/m6/results/discovery.md` output is untracked and will enter the source hash on the dev box; consider adding it to `.gitignore` patterns or committing it per repo convention before gate runs. Dirty runs are labeled `development-only` (`scripts/m1.py:209`) and cannot become Verified evidence; clean-tree guards remain `m0.clean`/`m1.source_hash`.
- A72 drills need a measured-claims flip: `deploy/profiles/local.json` `capacity_fixture.results` currently `"not-measured"` and `m0.check_config` only validates positivity/retention — the release validator must require the measured artifact set (upgrade/restore/load/security) instead of trusting profile text.
- A72/security-review and backup-restore have no code or doc today; both are net-new scope inside WP18 (spec §14.1 + milestone M6 task 2). Cheapest compliant shape: deterministic drill script under `tests/m6`/`scripts` + reconciliation JSON + runbook section update.
