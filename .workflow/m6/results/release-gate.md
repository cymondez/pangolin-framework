# M6 release gate — implementation report

Owner: release-gate worker (GLM-5.3). Scope: REL-003/A70, REL-005/A72,
ORG-014/A95 fail-closed release acceptance, the committed release manifest,
drill-report validation and the cross-platform M6 gate. Official status stays
**In progress**; nothing here claims Verified — that requires same-commit
two-platform gate evidence.

## Delivered files

| File | Role |
|---|---|
| `deploy/release/manifest.json` | Declarative v0.1.0 release manifest: baseline/version scope, 95 unique requirement deliveries, 421 subtests, 3 profile rows, runtime/CLI/template version bindings, 11 published event schemas, 11 critical coverage packages, 11 required assets, 5 drills, 2 gates |
| `deploy/release/README.md` | Release package README (validation entry points, version/schema policy, drill interface, fail-closed behavior, current boundaries) |
| `scripts/release_manifest.py` | Release library + `--check` CLI: 3-way delivery validation (manifest ↔ milestone plan §5 ↔ cases/subtests), 95/421 closure across m1–m6 mappings (unknown/duplicate/missing rejected), profile rows via `m0.check_profiles`, version bindings (`product_version` + CLI const + template `require` line), event schema binding (source existence + template-profile completeness + reverse emit scan; wire versions independent of product version), critical list cross-checked against `tools/check/main.go` and spec §13.3, asset presence, raw-count coverage thresholds (89.99 fail / 90.00 pass, aggregate cannot mask critical, non-int/NaN/negative/impossible counts rejected), drill report validation, machine-collected `release-manifest.json` evidence |
| `scripts/m6.py` | M6 verify (reuses m1 cumulative runner via `extra_steps` hook: `release-manifest` check → drills → bound evidence) + `m6.py gate --commit` validating both platforms |
| `scripts/m6_linux.py` | Fixed OCI Linux runner entry (`m3_linux.main(6)`) |
| `tests/contract/m6.json` | M6 mapping: 13 subtests = 8 python (A70×4, A95×4) + 5 drills (A72×5); `module_docs`: tests/m6 + deploy/release READMEs |
| `tests/m6/test_gate.py` | Negative-first gate regressions (8 mapped A70/A95 test names + drills provenance suite) |
| `tests/m6/README.md` | M6 test README with reconciled drill interface |
| `scripts/check.ps1` / `scripts/check.sh` | Full gate entrypoints now run `m6.py verify` (Linux always `--race`) |
| `.github/workflows/ci.yml` | CI: `m6` job (pangolin-m6-ci project, PANGOLIN_M6_PROJECT, services via m1+m5-databases compose, logs/artifacts/cleanup) + windows-portability job |

Minimal shared-file extensions (older behavior preserved; m0–m5 suites re-run green):
`scripts/m1.py` (template block `("M5","M6")`, `extra_steps` hook after coverage, `m6-regression` in the python-validation name set), `scripts/m5.py` (`validate_report(..., milestone=)` parameter, default `'M5'`), `scripts/m1_e2e.py` (m6 namespace — already landed by drills worker/lead), `scripts/m3_fixture.py` (broker restart milestone 6), `scripts/m3_linux.py` (milestone 6 + `PANGOLIN_M6_DRILLS_DIR` provenance forwarding into the OCI runner), `scripts/m5_compose.py` (m5/m6 project regex).

## Gate semantics (fail-closed)

- **A70/missing-release-item**: removing any required asset/delivery/drill/profile row/schema/gate fails the release and lists the missing items; absent asset files are listed individually. After the drills worker landed `docs/runbooks/release.md`, `docs/security/release-review.md` and `docs/release-notes/0.1.0.md`, `python scripts/release_manifest.py --check` passes end-to-end (95 deliveries, 421 subtests, 3 profile rows, versions, 11 schemas, 11 critical packages, all assets hashed, 421 closure).
- **A70/unknown-requirement / duplicate-delivery**: unknown requirement IDs, subtests mapped to wrong milestones, cross-milestone duplicate subtest mappings and two requirements claiming one case are all rejected.
- **A70/template-runtime-version**: runtime (`VERSION`), CLI (`internal/scaffold/cli.go` const) and template (`go.mod.tmpl` require line + `template_bundle.py --check` byte parity in the gate) must all bind 0.1.0; event schema wire versions are validated as their own integers and never compared to the product version (schema v1 with product 0.1.0 is the accepted published binding).
- **A95/organization-contract-fails**: `check_report_closure` requires every mapped unit/integration case in the report; a missing organization case fails the release with the exact test name.
- **A95/applicable-case-skipped**: every m0–m6 regression log is re-validated (`no skip, OK, all required python names`); skipped/failed drill entries reject; missing mapping coverage of any of the 421 subtests rejects.
- **A95/critical-package-below-threshold**: raw-count exact thresholds on overall and every critical package (11 prefixes, cross-checked against the Go coverage gate list and spec §13.3); missing critical package coverage cannot be hidden by aggregates.
- **A95/tenant-only-build-independent**: real `go list -deps ./internal/testkit/assemblies/tenant` must not contain any `modules/organization` dependency.
- Coverage/e2e/template/M4-asset and ancestry regression validation chains through `m5.validate_report(..., milestone='M6')` → `m4` → `m1` unchanged.

## Drill interface (reconciled with the drills worker)

`scripts/m6_drills.py` (drills worker, now landed) imports `release_manifest.DRILLS`/`THREAT_CATEGORIES` and emits the contract my `validate_drills` enforces: top-level `status/commit/source_sha256/working_tree_dirty/evidence_class/platform(+platform_details)/project/commands/artifacts`; per-drill `status/commands/checks`. My gate independently re-checks provenance (same commit + source bytes, `evidence-ready` for clean commits, inline platform match), every command log digest, every artifact digest (path-escape-proof), and the release-blocking `checks` (upgrade: seeded rows/owner preserved/FORCE RLS/no permissive policy; backup: restore verified/rows/checksums/independent connection; load: hardware/pool/load/concurrency/p95/p99/methodology + ≥10 raw samples, percentiles must not contradict samples, NaN rejected; security: all 12 §14.1 categories, `unresolved_high_risk=0`, controls with attack surface/abuse path/control/executable test/residual risk). The drills script reuses the running service project (`--project` = verify project) for its compose pg-client fallback; the gate passes `report["project"]` directly.

## Validation performed (narrow, this session)

- `python -m unittest discover` for **m0(8) m1(7) m2(4) m3(4) m4(4) m5(21) m6(24)** — all OK (24 in tests/m6 include the evidence-index worker's 14 `test_index` tests).
- `python scripts/m0.py check` — full contract inventory OK (API surface, imports, profiles, fixtures, dependencies).
- `python scripts/release_manifest.py --check` — now passes fully (95/421/3 profiles, versions, schemas, critical packages, all release assets hashed); before the drills worker's docs landed it failed closed listing exactly those missing assets, proving the A70 semantics.
- `go build ./...` and `go vet ./...` — OK (m0 environment).
- CLI argument guards (`m6.py gate/verify`, `m6_linux.py --project`), compile checks for all touched scripts, m6-regression python-name validation simulated against real discovery output.
- m1.verify M6 flow components were validated at unit level (closure, regression logs, evidence collection inputs); the **full service-backed verify was NOT run** — the lead runs real services and both platforms.

## Dependencies and limits

- **Drills**: `scripts/m6_drills.py` and the five Go drill tests are the drills worker's; `TestM6BackupRestore` additionally needs host pg clients (or `PANGOLIN_M6_PG_*` hints their script sets); the lead must provide pg clients on Windows before the cumulative integration suite runs tests/m6 (their runner image `deploy/docker/Dockerfile.m6-runner` covers Linux).
- **Pending release assets**: resolved — the drills worker delivered `docs/runbooks/release.md`, `docs/security/release-review.md` and `docs/release-notes/0.1.0.md`; the manifest check passes with all assets present.
- **Linux runner**: no Docker socket; host-run drills are adopted via `PANGOLIN_M6_DRILLS_DIR` only with identical commit/source bytes and per-file hash re-verification after copying.
- **Not done here** (by design): real-service verify/gate runs, race on both platforms, Docker service inventory/cleanup, Docusaurus edits, official status updates, `scripts/README.md` (lead-owned), spec/milestone-plan edits (lead-owned; the drills worker already set M6 rows to In progress).
- No commits, tags, pushes or deployments were made; the shared worktree retains the other workers' edits untouched.
