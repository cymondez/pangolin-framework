# M6 release drills — interface for the release gate

Owner: release-drills worker (GLM-5.3). Executable evidence for A72／REL-005:
clean rebuild, forward upgrade, backup/restore, load baseline and security
review. This file is the interface handoff; the authoritative schema
validator is `scripts/release_manifest.py:validate_drills` and the acceptance
authority stays with the specification (§13/§14/§16) and milestone plan
(WP18, §8).

## Public interface

```text
python scripts/m6_drills.py --output <dir> --project <project>
```

- `--output` (required): fresh evidence directory; it must not already
  contain `release-drills.json`. All logs, evidence JSON, dump and hashes
  live there.
- `--project` (required): must match `pangolin-m6-[a-z0-9-]+`. The gate passes
  `<verify project>-drills`; it identifies the drill run. The Docker pg
  client fallback uses the running compose project — derived by stripping the
  `-drills` suffix, or set explicitly via `--compose-project` /
  `PANGOLIN_M6_COMPOSE_PROJECT`.
- `--pg-bin`/`PANGOLIN_PG_BIN`, `--compose-file` (repeatable, default
  `deploy/compose/m1.yaml`): pg client and compose inputs for the
  backup/restore fallback.
- Exit `0` only with `status: "passed"`; everything else fails closed
  (command errors, skipped/missing tests, missing tools/services, missing or
  contradictory evidence).
- Environment (same as earlier suites): `PANGOLIN_M1_ADMIN_DSN`,
  `PANGOLIN_M1_APP_DSN`, `PANGOLIN_M1_WORKER_DSN`, `PANGOLIN_M1_NATS_URL`,
  `PANGOLIN_M1_ISSUER`. The script records non-secret endpoints only.

## Report contract (`<dir>/release-drills.json`)

Validated by `release_manifest.validate_drills`: top-level `status`
(`passed`), `commit`, `source_sha256` (m1.py working-tree bytes hash),
`platform` (`windows-amd64`/`linux-amd64`), `project` and `drills` — a map
with **exactly** the five names. Each entry has `status: "passed"`,
`commands` (each record **exactly** `argv`/`exit_code`/`log`/`sha256`,
relative log paths, all exits 0) and `checks`:

| drill | checks |
|---|---|
| `clean-rebuild` | `clean_source`, `clean_database`, `clean_build`, `tests_passed` (all true) |
| `forward-upgrade` | `seeded_tenant_rows` (int), `owner_preserved`, `force_rls` (true), `permissive_policy_left` (false) |
| `backup-restore` | `restore_verified`, `row_counts_match`, `checksums_match`, `independent_connection` (all true) |
| `load-baseline` | `hardware`, `go_version`, `database`, `broker`, `methodology` (text), `pool_size`, `load_count`, `concurrency` (int), `p95_ms`/`p99_ms` (positive), `samples` (raw list, ms) |
| `security-review` | `categories` (the 12 spec 14.1 names), `unresolved_high_risk` (0), `controls` (per-category `category`/`attack_surface`/`abuse_path`/`control`/`test`/`residual_risk`) |

Extra top-level and entry fields (provenance beyond the strict contract):
`interface`, `baseline`, `product_version`, `working_tree_dirty`,
`evidence_class`, `platform_details`, `compose`, `inputs` (hashes of VERSION,
go.mod/go.sum, toolchain, dependencies, security review doc), `environment`
(endpoints only), `services` (measured PostgreSQL/NATS versions), `error`,
`artifacts` (`{relative path: sha256}` for every file except the report).
The five entries also carry drill detail (`database` names, pg argv, RPO/RTO,
percentiles, build hashes, executable security tests).

## What each drill executes

- **clean-rebuild**: exports the current working-tree source bytes to
  `<output>/clean-source`, full `go build ./...` with a fresh
  `GOCACHE`/`GOTMPDIR`, then `tests/m6:TestM6CleanRebuild` from the export:
  unique fresh database (`m6_rebuild_*`) deployed with the real migration
  set, real Bookstore API/Worker binaries built from the export, real RS256
  JWT from the test IdP, HTTP write/read, 2xx-only-after-commit independent
  reconciliation (books + pending outbox), forged-token 401 and
  `X-Tenant-ID` forgery 403, worker outbox→JetStream→inbox→read-model drain
  on a drill-unique stream. Evidence `clean-rebuild.json` + `api-run.log`,
  `worker-run.log`, build logs. Export and caches are removed afterwards;
  the database and stream are dropped/deleted by the test even on failure.
- **forward-upgrade**: `tests/m6:TestM6ForwardUpgrade` — seeds the real M1
  tenant-only schema and data through app/worker RLS sessions in a unique
  `m6_upgrade_*` database, applies the complete real pending migration set
  (framework modules + organization + bookstore scope upgrade), then
  fresh-connection assertions: owner preserved as `tenant/TenantID` (no org
  guessed), projection/checksum equality, single `exact_owner` policy with
  FORCE RLS (no permissive union), old API probe fails, upgraded schema
  serves org-owned rows, cross-tenant probes zero. Evidence
  `upgrade-drill.json`.
- **backup-restore**: `tests/m6:TestM6BackupRestore` — unique seeded
  organization-enabled database, real `pg_dump -Fc` and `pg_restore` into a
  new `m6_restore_*` database, fresh-connection comparison of **every**
  table (owner/keys/counts/checksums via row_to_json digests), RLS
  flags/policies/roles restored, app-role probe, migration ledger restored,
  measured RPO window and RTO. pg client resolution: `--pg-bin` dir → PATH
  (version-checked ≥ server major) → `docker compose ... exec -T postgres`
  (container loopback DSN rewrite) → fail closed with the host-run procedure
  in `docs/runbooks/release.md`. Evidence `backup-restore.json`,
  `backup.dump`, `pg-*.log`.
- **load-baseline**: `tests/m6:TestM6LoadBaseline` — deterministic real
  framework write load (800 measured UOW ReadWrite operations, concurrency
  8, warmup 40, `pangolin_app` under FORCE RLS, parameterized INSERT +
  outbox), raw per-op samples, pool saturation samples, heap deltas; the
  script independently recomputes p50/p95/p99 and validates reconciliation.
  No throughput promises — measured numbers only. Evidence
  `load-baseline.json`.
- **security-review**: parses the machine-readable control block in
  `docs/security/release-review.md` (12 spec §14.1 categories), fails on
  any unresolved high risk, then executes every mapped test selector
  (unit: security/authn, security/policy, transport/http, observability,
  platform cache/settings/features, persistence/uow, bookstore session
  auth; integration: tests/m3 IDOR/governance/downgrade/inbox-lease/
  idempotency, tests/m4 store/capacity, tests/m6 security drill) requiring
  pass without skip. Evidence `security-drill.json` (Go drill) +
  `security-tests.json` (script-run results).

## Resource ownership

Each drill owns unique databases (`m6_<drill>_<random>`), created and always
dropped by the Go tests; the clean rebuild owns one JetStream stream
(created/deleted by the test) and a temporary source export plus build
caches (deleted by the script). The script itself never starts or stops
services and only uses `docker compose exec` for the pg client fallback
inside the already-running fixture `postgres` service. Nothing in the
shared fixture database is written by the drills; m3/m4 security evidence
tests use their standard idempotent fixture bootstrap like the cumulative
gate.

## Gate integration

- `scripts/m6.py` calls this script during verify with
  `--output <gate output>/release-drills --project <verify project>-drills`.
- `validate_drills` binds the report to the gate's `commit`/`source_sha256`,
  requires the platform match for inline runs, validates every command log
  hash and the checks above.
- Linux runners without Docker socket: the pinned runner image
  (`deploy/docker/Dockerfile.m6-runner`) ships pg 18.7 clients on PATH, so
  host mode resolves. Host-run drills are adopted via `PANGOLIN_M6_DRILLS_DIR`
  with explicit provenance validation (`adopt_host_drills`).
- Running `tests/m6` directly (cumulative integration suite) is safe with
  just `PANGOLIN_M1_*`; `TestM6BackupRestore` additionally resolves its pg
  client the same way as the script (hints → PATH →
  `PANGOLIN_M6_COMPOSE_PROJECT`, a single operator-set env var naming the
  inventoried fixture project; the runner image bakes pg clients, and on
  Windows hosts without pg tools set e.g.
  `PANGOLIN_M6_COMPOSE_PROJECT=pangolin-m6-dev` before the gate run — see
  the runbook).

---

## Completion report (release-drills worker, 2026-11-24)

### Delivered files (owned)

| file | content |
|---|---|
| `scripts/m6_drills.py` | drill orchestrator + fail-closed validation; emits the `release_manifest.validate_drills` schema |
| `tests/m6/helper_test.go`, `drills_test.go`, `upgrade_test.go`, `backup_test.go`, `load_test.go`, `security_test.go` | the five executable drills (+ shared helpers), isolated `m6_*` databases, evidence JSON |
| `docs/runbooks/release.md` | release drill runbook, RPO/RTO methodology, pg fallback + host-run procedure |
| `docs/security/release-review.md` | spec 14.1 review with the machine-readable control block consumed by the security drill |
| `docs/release-notes/0.1.0.md` | v0.1.0 release notes (scope, versions, risks, upgrade steps, evidence entry points) |
| `.workflow/m6/results/release-drills.md` | this interface + completion record |

### Exact checks executed (real services, working tree)

A complete drill run against the running `pangolin-m6-dev` fixture
(PostgreSQL 18.7, NATS 2.15.3, test IdP) produced a **passed** report at
`artifacts/m6-drills-full/release-drills.json` (development-only class: the
shared worktree is dirty with both workers' in-progress files):

- **clean-rebuild** — full `go build ./...` from an exported clean source
  tree with fresh caches (11.4s); `TestM6CleanRebuild` (17.1s): fresh
  `m6_rebuild_*` database via the real migration set, API/Worker binaries
  built from the export, real RS256 JWT write 201 + read 200, independent
  post-commit reconciliation (books=1, outbox pending), forged token → 401,
  `X-Tenant-ID` forgery → 403, JetStream delivery reconciled
  (book_views=1, inbox processed=1, outbox `sent:1`), both processes exit 0.
- **forward-upgrade** — 60 seeded tenant-only rows (2 tenants × 30, incl.
  soft-deletes) upgraded by the real pending migration set in one
  deployment run (versions 3000001…4000002, `compatible@4000002`);
  projection checksum identical pre/post, 0 owner violations, policies
  exactly `["exact_owner"]`, FORCE RLS true, catalog rows exact
  (no org guessed), cross-tenant probes 0, old-API probe fails,
  org-owned insert served.
- **backup-restore** — real `pg_dump -Fc` (70,202 bytes, sha256 recorded,
  0.25s) and `pg_restore` (0.43s) via container-mode `docker compose exec`
  (client 18.7); 17 tables compared on fresh connections with **zero**
  mismatches (owner/keys/counts/checksums); RLS flags, policy set, role
  posture and the 10-row migration ledger restored; app-role probe sees 25
  tenant-a rows on the restored database; measured RPO 0.094s window /
  0 rows missing, RTO 0.51s.
- **load-baseline** — 800 measured real UOW write operations, concurrency
  8, warmup 40, pool max 16; raw 800-sample log saved; p50/p95/p99 =
  5.49/31.0/37.2 ms (machine-specific, no promises); independent
  percentile recomputation matched; reconciliation 840 books/outbox rows,
  0 errors.
- **security-review** — 22 executable control tests passed (unit + real
  integration + `TestM6SecurityDrill`: 19 FORCE-RLS tables with 0
  violations, role posture clean, IDOR probes all zero, injection payload
  stored byte-exact); 12 categories, 0 unresolved high risk.

The report passed `release_manifest.validate_drills` (the release gate's
own validator) across provenance, per-entry command log hashes, artifact
hashes and every release-blocking check. Leftover drill databases verified
0 via an independent connection after all runs.

### Remaining limitations

- The passing run is **development-only**: the shared worktree is dirty
  (both workers' files); Verified-class evidence requires the lead's clean
  same-commit dual-platform acceptance. The run's `source_sha256` changes
  whenever the worktree changes — the gate recomputes and binds both at
  verify time.
- backup/restore ran via the Docker fallback on this host (no host pg
  clients); the Linux runner path (host pg clients baked into
  `Dockerfile.m6-runner`) is validated by resolution logic + version checks
  but not executed here.
- The measured load/RPO/RTO numbers describe this machine and seed; they
  are recorded as such and are not capacity or recovery promises.
- `pangolin_security.tenant_memberships` metadata exposure is a reviewed,
  accepted finding (see the security review document), not a drill failure.
- No Docusaurus edits (per packet); no commit/push/tag/deploy — all work is
  in the working tree for lead review.
