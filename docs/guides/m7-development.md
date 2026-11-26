# M7 development guide: full same-source realtime acceptance

Status: **In progress** (formal gate not yet run). This guide defines how the
M7 realtime acceptance is executed, what the runner enforces, and which
interfaces the Go/JS test slice must satisfy. The formal milestone status is
maintained by the milestone plan (§9); only a passing same-commit two-platform
gate (M7 `gate.json` in `artifacts/<commit>/m7/`) may claim Verified.

## 1. Architecture

M7 is a **true cumulative** acceptance. `scripts/m7.py verify` does not
re-implement any older gate; it reuses them:

- The whole engineering chain comes from `m1.verify` with the M7 mapping as
  `increment` (M2..M6 mappings + M7 contract): M0 inventory (scoped, exact
  104 groups / 593 named subtests), `go build/vet`, `go mod verify`, the
  m0..m7 Python regressions, unit + `-tags=integration` suites including
  `./tests/m7` and `./tests/database`, the M1 real-process E2E, sqlc
  generation pinning, and `-race` on Linux.
- Template/CLI coverage (M5 block) also runs for M7: `template_bundle --check`,
  a `-cover`-instrumented CLI, `m5_templates.py` real HTTP startup for both
  generated profiles (this is also the legacy (no-realtime) startup evidence
  for A104/legacy-off — a real HTTP start, never a compile test), and the
  generated Compose execution on Windows.
- The M6 release acceptance is injected through `m6.release_steps`: the
  committed release manifest re-validation, the five executable release drills
  (`PANGOLIN_M6_DRILLS_DIR` on Linux), the bound `release-manifest.json`, and
  the per-profile evidence index — all against the same source bytes.
- On top of that, M7 adds (all fail-closed):
  1. **official-client instrumented E2E** (`realtime_coverage_steps`): the
     `examples/realtime` binary built with `go build -cover
     -covermode=atomic -coverpkg=<all production packages>`, driven by
     `npm --prefix examples/realtime/client test -- --test-reporter=tap`
     (@microsoft/signalr 10.0.0, three release profiles, two independent
     server processes on real pinned PostgreSQL/NATS). `GOCOVERDIR` is
     inherited by both nodes and converted with `go tool covdata textfmt` to
     `e2e-realtime.out`.
  2. **raw atomic coverage union**: `merged.out` = unit + integration + M1 E2E
     + CLI-covered template run + realtime E2E; `tools/check -coverage`
     computes the release coverage from the union. No hand-written counts.
  3. **release coverage gate**: ≥90% raw statement coverage overall and per
     critical package — including every `modules/realtime/...` package and
     `events/nats` (additive manifest below).
  4. **additive release evidence** (`release-m7.json`): requirements 104
     (95 historical + RT-001..RT-009), 593 named subtests, three profile
     rows, versions, closure M1..M7, regressions, release drills summary and
     hashes.
  5. **spec 13.6 evidence index** (`profile-evidence-m7.json`): one row per
     subtest per profile (593 × 3), each required row bound to the exact raw
     artifact (JSONL log, Python log, templates.json, release-drills.json or
     realtime-e2e.json) with sha256; not-applicable rows keep their reason
     and no pass claim.
- `m7 gate` re-validates both platform reports, re-derives every artifact,
  compares the platform-independent declarations and writes
  `m7/gate.json`. A dirty tree keeps `gate: development-only` and the index
  class `development-only`; Verified requires both platforms `evidence-ready`
  from the same source commit and the same source bytes.

Evidence-ordering note: both indexes (`profile-evidence.json`,
`profile-evidence-m7.json`) and `release-m7.json` are written inside the
injected hook, before `m1.verify` finalizes
`report.artifacts`/`status`. Like the M6 index, the M7 index is written from
the `{**report, "status": "passed"}` projection, derives every proof from the
raw files (log and artifact sha256 are recomputed from disk), and the
finished report is strictly re-derived and compared byte-for-byte during
`validate_report` — the final artifacts map is cross-checked against each
bound log/artifact hash there.

## 2. Contract inventory (additive, frozen)

- `tests/contract/subtests.json` carries the 9 RT-001..RT-009 appendices
  (RT-001..RT-009 → A96..A104). `m0.check` validates them against the
  specification RT rows and the milestone plan rows; `pending` subtests stay
  documented in the inventory but **block the mapping and the gate**.
- The three profile manifests each cover all 593 named subtests. Only
  organization-owned semantics are plan-excluded on the tenant-only profile
  (required 512 / not-applicable 81: `A98/enterprise-child-negative`,
  `A98/multi-membership`, `A99/disabled-scope`); the tenant-side negatives of
  the same requirement groups (scope collisions, unsubscribe, user devices,
  credential/account/permission revocation, token binding) stay required.
  `organization-enabled` and `replacement-contract` keep every row required
  (593/0). The M7 release profile totals are derived from the manifests and
  asserted by the gate tests. The tenant-only plan never omits the
  requirement group itself — its M7 rows stay mapped in
  `tests/contract/m7.json`.
- `deploy/release/manifest.json` (historical 95/532) now lists the 12
  production critical prefixes from `tools/check` (adds `modules/realtime`);
  `deploy/release/m7.json` adds `events/nats` for the M7 scope. VERSION stays
  `0.1.0`; the realtime subsystem targets v0.2 and is **not** a tag release.

Root-freeze artifacts already applied in this tree: `tests/contract/api.json`
was regenerated with `python scripts/m0.py freeze-api` (the frozen public API
now covers `modules/realtime`, `modules/realtime/jetstream`,
`modules/realtime/signalr`, `examples/realtime`, `events/nats` and `kernel/app`;
ignored `artifacts/` working archives dropped out). The same-version runtime
bundle was regenerated with `python scripts/template_bundle.py` for the
current README/template state. The reviewed official-client
dependency/license inventory lives in
`deploy/release/m7-client-dependencies.json` (18 locked packages; MIT,
BSD-2/3-Clause, Unlicense) and is re-validated live by
`scripts/m7_client_dependencies.py` during the gate. If the test slice
changes exported Go APIs, bundled sources or the client dependency tree
again, the root re-runs `freeze-api`, `template_bundle.py` and the
client-license review before freezing.

## 3. Required mapping/evidence interfaces (test-slice contract)

- `tests/contract/m7.json` must end with:

- `milestone: "M7"`, `baseline: "v1.3"`, `pending: []`;
- `unit_tests` mapping the 35 named Go subtests to Go test identities of the
  form `github.com/cymondez/pangolin-framework/<pkg>:TestAxxRTxxx/<name>` that
  actually run in the unit (`-json`) stream — e.g.
  `.../modules/realtime/signalr:TestA96RT001/official-negotiate`;
- `template_tests` mapping the official-client/E2E named subtests to the
  single contract `examples-realtime-official-client` and, if legacy-off or
  generated-profile rows are moved out of unit tests, optionally to the
  historical M5 contract `both-profiles-build-test-start` (real HTTP startup
  of the no-realtime generated profiles); both identities are bound by the
  runner. A104/legacy-off must map to a real-HTTP-start identity, not a
  compile test;
- `integration_tests` (currently empty) may move broker-restart/redelivery
  cases to real-service Go integration identities run under
  `-tags=integration` in `./tests/m7`;
- `regression_tests`/`regression_integration_tests` for extra regression
  cases (empty allowed).

The Go tests must produce exactly these `-json` test identities (package path
+ `Test` name + `/` + subtest variant); any skip, missing case or extra
failing test fails the gate. `tests/m7/test_*.py` must run without skips
(the Python validator rejects `skipped=`).

The harness must be run exactly as documented: `npm --prefix
examples/realtime/client ci --ignore-scripts` then `npm --prefix
examples/realtime/client test -- --test-reporter=tap` with environment
`PANGOLIN_REALTIME_BINARY` (the covered demo binary built by the runner),
`PANGOLIN_REALTIME_DEMO_KEY`, and the `PANGOLIN_REALTIME_*` DSN/NATS
variables the runner derives from `PANGOLIN_M1_ADMIN_DSN`,
`PANGOLIN_M1_APP_DSN`, `PANGOLIN_M1_WORKER_DSN` and `PANGOLIN_M1_NATS_URL`.

The harness log is re-parsed strictly: the TAP summary block (`# tests`,
`# pass`, `# fail`, `# cancelled`, `# skipped`) must be complete and
consistent with the column-0 top-level results, and exactly one top-level
test per release profile must have run — a missing/truncated/contradictory
summary, a duplicate-profile or unknown top-level test, a failed, cancelled
or skipped case all fail the gate. The machine-written `realtime-e2e.json`
artifact is revalidated the same way.

### Realtime fixture preparation (recorded before the unit suite)

`m1.verify` gained a recorded `prepare` hook that runs before the unit
command; M7 binds `m7.prepare_realtime`. It creates a dedicated realtime
database `m5_template_m7rt_<12hex>` inside the isolated project's pinned
PostgreSQL (the `internal/testkit/cmd/template-db` fixture tool owns
creation; the pre-migrated M1/M3 fixture database is never reused), builds
the `-cover -covermode=atomic` (plus `-race` on Linux race runs) demo binary
from `./examples/realtime`, maps the `PANGOLIN_REALTIME_{BINARY,DEMO_KEY,
ADMIN_DSN,APP_DSN,WORKER_DSN,NATS_URL}` environment the acceptance tests
require, sets `GOCOVERDIR` to the shared `realtime-cov` directory and runs
`go run ./examples/realtime --provision`. The binary, environment and
coverage directory stay active for unit, integration, race and the
official-client E2E, so every subprocess run — the worker's
`./tests/m7` acceptance servers included — joins the same-source raw
coverage union (4514 child coverage blocks were recorded in the 2026-11-26
preflight). Missing pieces fail closed; the run records
`report.prepare.realtime` (database, binary, provisioned, race flags) which
`validate_report` re-checks against the artifact map.

Because each demo stream reservation is 64 MiB of the pinned NATS
`max_file_store` (256 MiB), and the default `PANGOLIN_REALTIME_DEMO` stream
created by `--provision` is never used by the suites, `prepare_realtime`
immediately frees exactly that stream through the demo binary itself
(`realtime-demo --cleanup-stream PANGOLIN_REALTIME_DEMO`, the fixture CLI
that verifies the exact literal stream route before deleting —
`realtime-broker-cleanup`). The official-client harness later removes its
own exact RT stream per profile in an explicit after hook, even on failure;
no wildcard cleanup tool exists in the runner. Afterwards the dedicated
realtime database is dropped (`realtime-db-drop`).

### Client dependency/license lock (realtime-client-licenses)

Right after `npm ci --ignore-scripts` and before the harness, the runner
records the reviewed npm dependency/license inventory:
`python scripts/m7_client_dependencies.py --check --output
<report>/client-dependencies.json` (`realtime-client-licenses`). The raw
artifact is bound evidence: `build_release_m7` re-reads
`client-dependencies.json` from the report directory and re-validates the
actual packages, versions, resolved URLs, integrity hashes, installed
versions and license-file digests against the committed
`deploy/release/m7-client-dependencies.json` (`m7.validate_client_
dependencies`) — metadata alone is never accepted, a tampered or drifted
artifact fails its own re-derivation. The manifest is a required M7 release
asset, its digest is bound in `report.inputs` and into
`release-m7.json.client_dependencies`, and the cumulative-gate proof
requires the command and the bound artifact.

### A104/cumulative-gates contract

The mapping field `gate_checks` binds `A104/cumulative-gates` to the single
supported contract `m7-cumulative-gates` (no pending entry remains in
`tests/contract/m7.json`; the blueprint row is planned again). Its proof in
the additive index (`m7_index._gates_proof`) binds executed cumulative
evidence with no circular report hash and no self-referential pass flag:
every required cumulative command (inventory, build, vet, module-verify,
m0–m7 regressions, unit, integration, E2E, M5 template block, sqlc, the
Linux race or Windows compose command, the M6 release-manifest/drills, and
the full realtime command family) must be present exactly once and exited
0; the recorded commit/source must be 40/64-hex with baseline v1.3; the raw
coverage union, go-list, exclusions, dependencies and templates artifacts
must carry 64-hex digests; overall raw statement coverage must reach 90%;
and representative command logs (integration, race or coverage, release
manifest) are re-read from disk against their bound sha256. The same-source
Windows+Linux gate remains the final authority.

## 4. Two-platform execution

### Windows (host services; drills inline)

1. Prepare the same-source fixture services (PostgreSQL 18.7, NATS 2.15.3,
   test IdP) for an isolated project, e.g.
   `docker compose -p pangolin-m7-win -f deploy/compose/compose.yaml up -d --wait`
   (or `deploy/compose/m1.yaml` as in M6), then export the fixture
   environment exactly as M6: `PANGOLIN_M1_ADMIN_DSN`, `PANGOLIN_M1_APP_DSN`,
   `PANGOLIN_M1_WORKER_DSN`, `PANGOLIN_M1_NATS_URL`, `PANGOLIN_M1_ISSUER`,
   `PANGOLIN_M1_TOKEN_URL`, `PANGOLIN_JWKS_URL`,
   `PANGOLIN_DATABASE_POSTGRES_DSN`, `PANGOLIN_DATABASE_MYSQL_DSN`,
   plus `PANGOLIN_M7_DEMO_KEY` (local demo key for the official-client
   harness) and `PANGOLIN_SQLC` if sqlc is not in `.cache/bin`.
2. Run the full gate: `python scripts/m7.py verify --project pangolin-m7-win`
   (or `scripts/check.ps1 -Project pangolin-m7-win -Milestone 7`).
3. Run the five release drills once per source: `python scripts/m6_drills.py
   --output <artifact dir> --project pangolin-m7-win` (inline on Windows; on
   Linux they are adopted from the host, see below).

### Linux (pinned runner)

1. Run the five release drills on the host for the same source and export
   `PANGOLIN_M6_DRILLS_DIR=<host drill report dir inside the repo>` and
   `PANGOLIN_M7_DEMO_KEY=...` (plus the fixture DSNs as for M3..M6).
2. Build the compose services for the isolated project
   (`pangolin-m7-linux`), then run the pinned runner:

   ```
   python scripts/m7_linux.py --project pangolin-m7-linux
   ```

   This builds the digest-pinned `Dockerfile.m7-runner` image (Go 1.27.4 +
   PostgreSQL 18.7 clients copied from digest-pinned images + a digest-pinned
   Node.js runtime — `node:22-bookworm@sha256:81ad3f93f122008a146891525948c689cfd8e9ff9f8cb8445cc95991d7a0251c`,
   the immutable linux/amd64 image manifest of the official Node image,
   verified with `docker buildx imagetools inspect`; node/npm versions are
   printed at build and again logged by the gate), clones an exact checkout of
   the working tree, and executes
   `python3 scripts/m7.py verify --project pangolin-m7-linux --race --external-broker-fixture`
   with `PANGOLIN_MILESTONE=7` (the official-client npm cache mounts on the
   shared `/cache` volume).
3. Freeze the commit, then verify and gate on both platforms:

   ```
   python scripts/m7.py gate --commit <full SHA>
   ```

Reports land in `artifacts/<commit>/m7/windows-amd64|linux-amd64/report.json`
(plus `release-m7.json`, `profile-evidence-{m6,m7}.json`,
`realtime-e2e.json`, `e2e-realtime.out`, and the command logs). Development
(inspecting) runs write `artifacts/m7-development-<sha12>/` and can never be
promoted; only clean same-commit runs become evidence.

## 5. Static, checksum, licensing and package inventory gates

Static analysis and the package inventory derive their exact package list
from tracked source files (`git ls-files … -- *.go`) instead of `./...`:
ignored generated/bug-reproducer directories under `artifacts/` never enter
`go build/vet/test/race` or the coverage inventory, while every tracked
package of the main module is kept (`m0.go_packages` for the default
constraints, `m0.go_packages(tags=True)` for `-tags=integration` runs,
`m0.go_build_packages` for the build step which rejects test-only packages
the historical `./...` skipped silently; `tools/check main.go` reproduces the
same selection for the coverage gate). Nested modules inside the tree
(`examples/basics`, `examples/moduleapp`) stay excluded exactly like the
historical patterns; the zero-test production packages remain in the
coverage denominator. A module genuinely outside a git repository
(temporary coverage fixtures) keeps the earlier `git-less` `go list`
inventory semantics in both implementations, while any other git
resolution error fails closed — no silent partial fallback. `go vet ./...`, sqlc `v1.31.1` generation drift and the
module/license allowlist (checksums + reviewed license files) run inside the
cumulative gate. Evidence checksums (sha256 of every command log and
artifact) are recomputed and re-verified by `m7 gate`; any mismatch fails
closed.

## 6. Historical evidence

M0–M6 evidence and gates stay byte-for-byte unchanged in semantics: the M6
closure still expects exactly 532 named subtests (scoped by the newest
milestone in the mappings), `release-manifest --check` still binds 95
requirements / 532 subtests / 95 deliveries, and the M6 gate remains the
release gate for v0.1.0. M7 only *adds*.
