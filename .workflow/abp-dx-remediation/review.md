# P3 Integrity Review

## Round 2 (2026-11-26)

Verdict: CHANGES NEEDED

- P3-001 | blocking | open | internal/scaffold/p3.go:111-132, internal/scaffold/p3_test.go:16-63. The direct user-module import is now present and the catalog/order happy path is built in the test, but imports remain unaliased. Legal registered module names `application`, `foundation`, or `module` collide with the generated own-application/foundation/kernel-module imports, so `module add --preset service` can emit uncompilable Go for valid input. Alias every user-module import (and each matching `module.Of` reference), and add a generated-project build case using a colliding legal dependency name.
- P3-002 | blocking | open | internal/scaffold/cli.go:411-486, internal/scaffold/p3.go:445-504. `p` and its expected registry are read before the project lock at `cli.go:452`; the post-lock reads only snapshot newer bytes while generation still uses the stale `p`. A second process can therefore wait, acquire the lock, and overwrite modules committed after its earlier read. The caller also replaces the commit/rollback error with `module generation failed; existing project files preserved`, hiding rollback failure and making the preservation claim unreliable. Acquire the lock before reading `pangolin.json` and registry state, rebuild the in-memory project and expected registry under that lock, retain/wrap commit and rollback causes, and add interleaving plus rollback-failure regression tests.
- P3-004 | blocking | open | internal/scaffold/p3.go:705-713, internal/scaffold/p3_test.go:170-207. The production path now checks `entry.Applied`, but the tests only cover an applied entry and a ledger read error. No case proves that a successful read containing only `Applied:false` entries fails, so the original len-only bug is not guarded. Add a live-doctor case with `ReadMigrationLedger` returning false entries and no error, and assert the migration-ledger check fails.
- P3-005 | blocking | open | internal/scaffold/p3.go:620-660, internal/scaffold/p3_test.go:160-207. Compose JSON parsing was added, but service/state fields are overwritten nondeterministically when both `Service`/`Name` or `State`/`Status` exist in a record, and any name containing `api` is accepted. The tests cover only a one-record array with running `api`; they do not prove that exited API plus a running unrelated service fails or that JSON-lines output is parsed. Read canonical fields with deterministic fallback precedence, match API container/service names deliberately, and add array/JSON-lines plus exited/non-API negative cases.
- P3-003 | blocking | resolved | internal/scaffold/cli.go:438-441, internal/scaffold/p3_test.go:107-113. Self-dependency reaches registry planning and remains classified as `module cycle:`; the returned error keeps the original classification and appends candidates. The regression assertion covers the cycle text.

Resolved IDs: P3-003.

Scope note: P3 owned files reviewed are `internal/scaffold/cli.go`, `internal/scaffold/p3.go`, `internal/scaffold/p3_test.go`, `internal/scaffold/cli_test.go`, `internal/scaffold/README.md`, `docs/site/content/reference/cli.md`, and `docs/site/content/tutorials/generated-bookstore.md`. No P4/upgrade-plan/runtime/verify content was touched by P3. No P3-owned change touches `kernel`, `runtime`, `application`, `persistence/uow`, `design`, `docs/evidence`, or `artifacts`; same-worktree edits in P1/P2-owned framework files are outside this packet.

Validation scope: complete P3 source/diff and existing P3 test evidence were read. No build, test, or restore was rerun, per the review request.

## Round 3 (2026-11-26)

Verdict: CHANGES NEEDED

- P3-002 | blocking | open | internal/scaffold/p3.go:423-433. When the staged-file rename fails after the original was moved to a backup, `replaceStagedRegularFile` discards the restore rename error with `_ = os.Rename(backupName, path)`. A failed restore can therefore leave the project file missing while the caller reports only the primary rename error and cannot preserve the rollback failure cause. Join the restore error into the returned error, and add a deterministic regression case that forces restore failure.

Resolved IDs: P3-001, P3-003, P3-004, P3-005.

P3-001 is resolved by aliased user-module imports/references in both service and default generation while the `realtime` package remains unaliased; P3-003 preserves cycle classification; P3-004 rejects a successful ledger containing no applied entry; P3-005 uses deterministic canonical field precedence, deliberate API service-name matching, array/JSON-lines parsing, and exited-state rejection.

Scope note: the P3 packet is limited to `internal/scaffold/cli.go`, `internal/scaffold/p3.go`, `internal/scaffold/p3_test.go`, `internal/scaffold/cli_test.go`, `internal/scaffold/cli_foundation_test.go`, `internal/scaffold/README.md`, `docs/site/content/reference/cli.md`, and `docs/site/content/tutorials/generated-bookstore.md`. No P4 upgrade/plan/dry-run/runtime/verify implementation or prohibited `design`/`docs/evidence`/`artifacts` content was modified by P3. Same-worktree `application`/`kernel`/`persistence/uow` edits belong to the earlier P1/P2 packets and are not P3 changes.

Validation scope: read-only review of the complete P3 source and diff plus existing neighboring realtime generation coverage. No source modification, build, test, vet, restore, or dependency operation was performed.

## Round 4 (2026-11-26)

Verdict: CHANGES NEEDED

- P3-002 | blocking | open | internal/scaffold/p3_test.go:109-154, internal/scaffold/p3.go:427-432. The production rollback path now preserves the restore rename cause with `errors.Join(err, rollbackErr)`, and the caller wraps the joined error. However, the required deterministic restore-rename-failure regression is still absent: the stale-registry test only covers rejection before commit, and no test hook or case forces `os.Rename(backupName, path)` to fail. Add a deterministic injected rename-failure case and assert that both the staged rename and rollback restore errors remain discoverable.

Resolved IDs: P3-001, P3-003, P3-004, P3-005.

Scope note: rechecked the complete P3 owned-file set. P4 upgrade/plan/dry-run/runtime/verify behavior is absent from the P3 packet, and no P3-owned change touches prohibited `design`, `docs/evidence`, or `artifacts`; same-worktree framework/template edits outside the packet remain P1/P2-owned.

Validation scope: read-only source, diff, and existing test review only. No source changes, build, test, vet, restore, or dependency operation was performed.

## Round 5 — P4 (2026-11-27)

Verdict: CHANGES NEEDED

- P4-001 | blocking | open | internal/scaffold/upgrade.go:644-765. `upgradeRuntime` builds classifications once, then re-reads `pangolin.json` and `framework/` but continues to apply the stale `plan.Changes`; a file classified `update` can therefore be edited after planning and still be overwritten by target bytes. Acquire a project mutation lock before the authoritative read and hold it through apply, revalidate the framework/manifest/project snapshots immediately before the first rename, and refuse if the classifications no longer match; coordinate the lock with the existing module-generation mutation path.
- P4-002 | blocking | open | internal/scaffold/upgrade.go:822-845. `upgrade verify` validates embedded runtime assets and the manifest but never checks `p.Version == Version`, even though `readUpgradeProject` deliberately accepts non-current versions. A partially upgraded project with target framework bytes but stale `pangolin.json` can therefore print `[PASS] upgrade verify`. Reject any `p.Version` different from the target version before target-manifest verification and make the error identify the stale project metadata.
- P4-003 | blocking | open | internal/scaffold/upgrade.go:867-883. The advertised no-write verify runs real `go build`, `go test`, and `go vet` without forcing read-only module resolution, so a missing/tidy-required module entry can let the Go tool update project `go.mod`/`go.sum` before verify returns. Run these checks with `-mod=readonly` (or an equivalently enforced command environment) and fail with the same explicit command on any required module-file update; do not treat injected no-op command functions as proof of production no-write behavior.
- P4-004 | blocking | open | internal/scaffold/upgrade.go:58-62. When a base-managed file is removed by the target and is already absent locally, the three-way classifier returns `conflict`. Local and target agree on deletion, so this is a normal no-action `unchanged` case, not a conflict; as written a safe repeated/manual deletion blocks every upgrade plan and runtime. Return `unchanged` for `baseExists && !targetExists && !localExists`, and retain `remove`/`conflict` for local-equals-base/local-diverges respectively.
- P4-005 | blocking | open | internal/scaffold/cli.go:125, internal/scaffold/upgrade.go:100-140. The upgrade dispatcher replaces the caller's context with `context.Background()` for both mutating runtime and verify commands. Cancellation or deadline after CLI dispatch is ignored, so runtime can continue replacing files and verify can keep running process checks after its caller is canceled. Pass the `CLI.run` context into `upgrade`, then into `upgradeRuntime`/`upgradeVerify`; check `ctx.Err()` before mutation and use it as the parent of each command timeout.

Resolved IDs: none.

Scope note: reviewed the complete P4-owned change set named by the task, including tracked `cli.go`/`cli_test.go`/documentation diffs and untracked `upgrade.go`/`upgrade_test.go` plus progress details. P1-P3-only framework/template changes were not attributed or reviewed.

Validation scope: read-only source, diff, tests, task context, and progress-details review. No source modification, build, test, vet, restore, or dependency operation was performed.

## Round 6 — P4 re-review (2026-11-27)

Verdict: PASS

Resolved IDs: P4-001, P4-002, P4-003, P4-004, P4-005.

- P4-001 is resolved: runtime acquires the same `.pangolin-module.lock` used by module generation before reading authoritative state, holds it through apply, rebuilds and compares the plan immediately before the first rename, and refuses any changed classification. Apply and restore remain lock-protected.
- P4-002 is resolved: verify rejects a `pangolin.json` version different from the target before manifest or target-runtime checks.
- P4-003 is resolved: production verify invokes build, test, and vet with `-mod=readonly`; `runCommand` uses `exec.CommandContext`, while the no-write path leaves project files unchanged.
- P4-004 is resolved: a base-managed file removed from target and already absent locally classifies as `unchanged`; the existing test covers the case.
- P4-005 is resolved: `CLI.run` passes its context into upgrade, runtime checks cancellation before mutation and at apply boundaries, and verify derives every command timeout from the caller context.

Apply/restore note: backup creation precedes mutation, each failed rename/checkpoint invokes restoration, old files are restored by rename with backup-copy fallback, and rollback errors are joined with the primary apply error. No material correctness defect remains in the inspected P4-owned apply, restore, plan/dry-run, or verify paths.

Scope note: re-reviewed only `internal/scaffold/cli.go`, `internal/scaffold/upgrade.go`, `internal/scaffold/upgrade_test.go`, `internal/scaffold/cli_test.go`, `internal/scaffold/README.md`, `docs/site/content/reference/upgrading.md`, and `.workflow/abp-dx-remediation/progress-details.md`, plus the directly used project-lock/command helpers in P3-owned code needed to verify P4 behavior. P1-P3-only behavior was not attributed to P4.

Validation scope: read-only review of the complete P4 source/diff, tests, documentation, workflow evidence, lock/command implementation, and restore control flow. No source modification, build, test, vet, restore, or dependency operation was performed.

## Round 7 — full worktree P1-P4 exit review (2026-11-27)

Verdict: CHANGES NEEDED

- P1-006 | blocking | open | application/operation.go:36-50. `executeResult` checks a nil Manager but never checks a nil callback. A non-nil Manager plus nil callback reaches `callback(work)` and panics instead of returning a classified error and a zero value. Fix: reject nil callbacks before calling `Within` (a focused invalid-options sentinel or new callback sentinel), and cover both CommandResult and QueryResult plus legacy Command/Query paths.
- P1-007 | blocking | open | tests/contract/api.json:8-28, kernel/module/facade.go:14-79. The worktree adds public `CommandResult[T]`, `QueryResult[T]`, `Resolve[T]`, `MustResolve[T]`, `AddSingleton`, `AddTransient`, and `AddOperation`, but the frozen API inventory has no matching signatures for these symbols. This leaves `m0.py check`/api freeze expectations, release inventory, and public API migration evidence inconsistent with the shipped source. Fix: regenerate and review `tests/contract/api.json` from the complete public surface before treating P1/P2 as acceptance-complete.
- P3-006 | blocking | open | internal/scaffold/p3.go:570-574. Static and live `doctor` run `go build ./...` without `-mod=readonly`; despite static/live output and DSN secrecy, Go module resolution can update project `go.mod`/`go.sum` in a command advertised as a diagnostic. Fix: enforce `-mod=readonly` for doctor build (and consistently for the live build call), reject/diagnose module-file changes, and add a snapshot/no-write regression with real production command arguments.
- P4-006 | blocking | open | internal/scaffold/upgrade.go:149-160,246-274,312-360. `readUpgradeProject` accepts both valid profile values and `targetUpgradeManifest` immediately uses the current `pangolin.json` Profile/Realtime fields to choose embedded framework assets, with no baseline-profile check or explicit profile-switch rejection. A manual change from tenant-only to organization-enabled can therefore make organization runtime assets enter as `add`/update and replace the framework without migration or compatibility confirmation, contrary to the documented switch restriction. Fix: bind upgrade target assets to a manifest or project baseline profile field, infer/reject baseline profile changes (including Realtime if it is a capability switch), and add plan/runtime regressions for tenant-only ↔ organization/Realtime switch attempts.

Open assumptions:

- Manual `pangolin.json` profile/realtime edits are considered upgrade inputs; the implementation does not currently authorize or document them as a supported in-place switch.
- No separate scenario-instructions or task.md was present for this full-worktree review; findings are grounded only in the current source, diff, existing review record, and frozen inventory.

Passes with no open finding:

- UOW sentinel identity and foreign same-text error classification are implemented and covered by the UOW regression test.
- `CommandResult`/`QueryResult` return callback values only after `Within` succeeds and return the corresponding zero value on callback/commit errors; no additional commit/callback regression was established beyond the nil-callback defect above.
- Root `Runtime.OpenScope`/`module.OpenScope` pins owner binding and rejects wrappers/raw containers; legacy `Runtime.OpenScope` remains the separate container-level compatibility view.
- Facade wrong-type/nil resolver/nil context/lifetime conflicts are guarded; typed-nil resolved pointer behavior matches the existing explicit test contract.
- P4 lock, snapshot revalidation, stale version, readonly verify, three-way removed/already-missing case, context cancellation, backup/restore control, and unsafe manifest path handling remain materially resolved in the inspected implementation.
- P3 module staging/lock and live doctor DSN secrecy are implemented as reviewed; P3-002 remains a deterministic restore-failure test gap, not a newly established code defect in this pass.

Scope note: reviewed the complete current HEAD-relative worktree P1-P4 change set, including untracked P3/P4 implementation files, then followed directly affected implementation, templates, tests, docs, and API inventory. The review did not modify source, rerun the existing tests, or alter execution evidence.

## Round 8 — P3-006 re-review (2026-11-27)

Verdict: PASS

Resolved IDs: P3-006.

- P3-006 | blocking | resolved | internal/scaffold/p3.go:546-625, internal/scaffold/p3_test.go:215-292. Production static doctor snapshots `go.mod` and `go.sum`, invokes exactly `go build -mod=readonly ./...`, and rejects a before/after snapshot difference on both command success and command failure. `doctor --live` runs this static path before live dependency checks. The regression uses an exact readonly argument assertion, compares full `snapshotTree` results around the successful static run, proves a deliberate `go.sum` mutation is rejected, and continues through a passing live path with exact readonly build arguments.

Scope note: reviewed exactly `internal/scaffold/p3.go` and `internal/scaffold/p3_test.go` for P3-006. The finding is resolved entirely in those files; `internal/scaffold/upgrade.go` and the CLI parser required no P3-006 changes and were not modified as part of this review. Other open findings from earlier rounds are outside this scoped re-review.

Validation scope: read-only review of the production command path, snapshot logic, test assertions, full-tree snapshot helper, live doctor call chain, scoped worktree status, and existing evidence in `.workflow/abp-dx-remediation/progress-details.md` (`go test ./internal/scaffold ./cmd/pangolin: PASS`). No build, test, vet, restore, or dependency operation was run.

Review file: d:\projects\side-project\pangolin-framework\.workflow\abp-dx-remediation\review.md

## Round 9 — P1-008 final re-review (2026-11-27)

Verdict: PASS

- P1-008 | blocking | resolved | application/operation.go:11-83, application/operation_test.go:68-243, internal/scaffold/assets/runtime/application/operation.go.tmpl:11-83. `Command`/`Query` and `CommandResult`/`QueryResult` reject literal nil callbacks before `Within`; `executeResult` rejects nil and typed-nil context/manager values and returns `uow.ErrOptionsInvalid` with the corresponding zero value. The callback result is retained only while `Within` succeeds; callback and commit errors return the corresponding generic zero. Literal nil and pointer typed-nil context/manager cases are covered, and both source and runtime template contain the same helper implementation.

Resolved IDs: P1-006, P1-007, P1-008, P4-006.

- P1-006's nil-callback panics are resolved by public prechecks and `executeResult`'s callback check.
- P1-007's public API inventory is now synchronized: `tests/contract/api.json` contains `CommandResult` and `QueryResult`, and the current module facade inventory includes the typed resolve/registration surface.
- P4-006 is resolved in `internal/scaffold/upgrade.go`: baseline profile/realtime capabilities are inferred from the local manifest and checked against `pangolin.json` before target assets or runtime mutation are used; profile/realtime mismatch regressions exist in `upgrade_test.go`.

Remaining test gaps (advisory, not blocking source findings):
- The typed-nil helper should gain explicit named nil-able kind cases for `Chan`, `Func`, `Map`, and `Slice` where they implement `context.Context` or `uow.UnitOfWorkManager`; pointer nil and literal nil paths are already covered, and the helper's common reflect-kind switch is complete for those practical interface implementations.
- P3-002 retains only a deterministic restore-rename-failure test gap; the production join/wrap path is implemented, but no injected rename failure proves the rollback restore cause in one narrow regression.
- No separate test gap is established for callback/commit zero-value behavior: callback and commit failures already return generic zero values, and template/source parity is exposed.

Scope note: re-reviewed the complete P1-008 owning path plus the runtime template and focused test; also rechecked open P1/P4 findings and the full worktree change set. No test, build, vet, restore, source edit, commit, or dependency operation was performed for this pass. The known baseline/API/upgrade findings named in earlier rounds were rechecked against the current files rather than treated as new defects.

Validation scope: read-only `git status`, `git diff`, `git diff --check`, source/template/test/diff review, and review-record continuity. Existing template check and focused test PASS evidence was used as prior evidence, not rerun in this pass.

Review file: d:\projects\side-project\pangolin-framework\.workflow\abp-dx-remediation\review.md
