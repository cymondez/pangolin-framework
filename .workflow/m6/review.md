# Lead integration and acceptance review

Review the complete final diff and the surrounding callers before accepting worker output.

- Preserve M0 inventory plans and historical fail-closed behavior; actual release evidence is a separate same-source collector.
- M6 namespace support must propagate to E2E, broker barrier, Linux runner, generated Compose and prior negative tests. E2E support and M5 negative regression were fixed by lead (21 tests pass).
- Cumulative M6 includes M5 templates/CLI instrumentation plus corrected real PostgreSQL/MySQL/SQLite contracts; check >=5 branches rather than ==M5.
- Align drill report schema with the actual scripts/m6_drills.py output, including platform shape, checks vs evidence, dirty status, command and artifact hashing.
- Linux runner has no Docker socket. Backup clients can be host-run with explicit source and database provenance, or available native clients; required backup tests cannot silently skip inside integration or race.
- Verify real pg_dump/restore, table data and owner hashes, RLS and independent connections; include measured recovery window/RTO and reliable pending-work reconciliation.
- Validate raw samples and recompute percentiles, record actual hardware/versions/pool/load/method limits; reject NaN, empty samples and contradictory metrics.
- Every applicable subtest has execution-backed evidence in all three profile rows; no invented passed flags, unknown IDs, duplicates or required skips.
- Release manifest ties required assets, toolchain, runtime/CLI/template/product versions and separately versioned event schemas to exact bytes. Reject path escapes and nonfinite/malformed data.
- Security record covers every specification 14.1 category with executable tests; no unresolved high risk.
- Overall and every actual critical production package meet raw-count threshold; missing critical package cannot be hidden by aggregate coverage.
- Before formal acceptance, workers stop editing; review/commit source, recheck clean tree, then run Windows and fixed Linux with full race from identical bytes. Gate verifies all logs/hashes and source identity.
- Only after runtime acceptance update Docusaurus, run consumer examples + site test/build, inspect beginner navigation and preserve running docs service.
- Inventory and clean only owned M6 containers/networks/volumes/unused dedicated images; retain logs.

## Initial integration observations (recheck final worker output)

- `release_manifest.check_versions` reused the regex match for the template when returning the CLI version; preserve the actual CLI version separately.
- `release_manifest.validate_drills` initially expected platform as string and `checks`, whereas drill worker's published interface uses platform object and `evidence`; align with actual final report and validate dirty/evidence class plus measured data, rather than adding synthetic passed flags.
- `m6.release_steps` initially used a suffixed `-drills` project even though the drill script uses the existing service project for pg clients. Use the actual service ownership namespace.
- `collect_evidence` reads report.artifacts before m1 finalizes them in its hook; snapshot the raw artifact hashes before collection or make collection hash directly and bind later.
- `TestM6CleanRebuild` initial readiness poll calls a request helper that Fatals on connection refusal, preventing retry; transient startup must return false while actual HTTP operations remain strict.
- Linux native clients are now available in lead-owned pinned `Dockerfile.m6-runner`; integrate runner build/use after worker completes, retain image provenance. Windows cumulative integration needs pg client container hints before the M6 tests, not only in the later drill script.
- Review the early `release_coverage` and latency validation for nonfinite/noninteger/negative/impossible counts and recompute p95/p99 from the saved sample set.

## Development runtime checks by lead

All five new Go drills independently passed on actual Windows/PostgreSQL/NATS after fixture corrections: load 1.37s, security 0.69s, forward upgrade 2.55s, pg_dump/restore 3.63s, clean API/worker rebuild 17.68s. Logs in ignored artifacts/m6-*.log. These are dirty development checks, not formal release evidence.

Lead fixed new-database schema setup/grants in helper_test.go, app-role outbox seeding and required view version in upgrade_test.go, excluded Goose metadata version 0, and made the clean rebuild readiness poll tolerate initial connection refusal. Preserve these fixes.

Pinned Dockerfile.m6-runner builds successfully; pg_dump/pg_restore/psql 18.7 are native in that image. Remaining integration: use/build the project-owned runner for M6 in m3_linux.py. Windows m6 verify must configure PANGOLIN_M6_PG_MODE=container and PANGOLIN_M6_PG_CONTAINER_ARGV before cumulative integration (as lead's ignored .cache/run-m6.py currently does for focused checks). Native clients solve Linux full integration and race without needing host drill adoption.

Final interface recheck: drill report platform object vs string; --allow-dirty supported if gate passes it; per-drill evidence content and raw sample consistency; report.artifacts available during release hook. If retaining optional host adoption, validate_report must not demand an inline release-drills command when actual verified host evidence was adopted.

## Integrated resolution before formal acceptance

- All workers have stopped; the drills worker's final provider request hit the GLM-5.3 session usage limit after delivering implementation and passing development evidence. No Copilot-hosted model was substituted.
- The five Go drills passed against real services. Lead and worker corrected fresh-database grants, seed-role/version fields, readiness retry, Goose metadata marker, and deterministic aggregate ordering. Existing production runtime behavior was not weakened.
- Native pinned PostgreSQL clients are built into the owned M6 Linux runner. Automatic pre-suite client selection and a CI fallback for older host clients are covered by focused Python tests.
- Profile index generation/revalidation is connected to the release gate. Counts/percentiles must match complete raw samples; nonfinite/impossible data and source/artifact path escapes are rejected. CLI inventory now points at the actual M6 gate rather than claiming every runtime case is still pending.
- Module/tool/script/test READMEs, development guide, release notes and runbooks are maintained. Recovery measurement explicitly describes the quiescent seeded dataset and does not promise production RPO. Docusaurus remains untouched pending formal acceptance.
- Next: freeze/commit this source, start fresh owned M6 services, collect native Windows and pinned Linux/full-race cumulative evidence, validate the same-source gate, then update public documentation.
