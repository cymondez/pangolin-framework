# P4 Progress Details — 2026-11-26

## Changed files
- internal/scaffold/cli.go: upgrade command/help dispatch and shared embedded asset filtering.
- internal/scaffold/upgrade.go: three-way upgrade plan/dry-run/runtime/verify implementation.
- internal/scaffold/upgrade_test.go: pure classification/path tests plus no-write/conflict/backup/verify regressions.
- internal/scaffold/cli_test.go: upgrade help and unknown-argument coverage.
- internal/scaffold/README.md: upgrade workflow and safety policy documentation.
- docs/site/content/reference/upgrading.md: plan/dry-run/runtime/verify, backup/rollback, and manual compatibility review documentation.

## Validation
- go test ./internal/scaffold: PASS.
- go test ./internal/scaffold ./cmd/pangolin: PASS.
- go vet ./internal/scaffold ./cmd/pangolin: PASS.
- python scripts/template_bundle.py --check: PASS (`same-version runtime bundle verified`).
- git diff --check: PASS.
- Clean temp project smoke: PASS; old framework/VERSION 0.0.9 plus matching old base manifest classified update, runtime created backup, and verify passed build/test/vet.
- Test injected verify failure: PASS; P4 test verifies actionable `go test ./...` command.

## Runtime behavior
- plan/dry-run are read-only.
- conflict/missing/invalid manifest/unsafe path block plan or runtime as appropriate.
- runtime creates `.pangolin-backup/<UTC timestamp>/` and emits direct PowerShell rollback commands.
- apply failure restores framework, manifest, and pangolin.json; rollback errors are joined with the apply error.
- verify performs no writes and runs build/test/vet after manifest and target checks.

## Reviewer follow-up
- P4 runtime now acquires `.pangolin-module.lock`, holds it through apply, rechecks the authoritative plan immediately before first rename, and refuses stale snapshots.
- Verify rejects stale `pangolin.json` versions and runs build/test/vet with `-mod=readonly`.
- Three-way removal with both base and target missing locally is `unchanged` rather than conflict.
- Runtime and verify propagate the caller context and check cancellation before mutation.

## Final verification
- Clean temp project smoke rerun after reviewer fixes: PASS; plan/dry-run/runtime/verify completed, readonly verify build/test/vet passed, timestamp backup and rollback output emitted, temp root removed.

## Review outcome
- IntegrityReviewer Round 6: PASS.
- P4-001 through P4-005 resolved: locking/snapshot revalidation, stale project version, readonly Go commands, removed-but-missing classification, and context propagation.
- Review record: `.workflow/abp-dx-remediation/review.md`.

## Remaining risk
- Apply rollback is copy/rename based and should be observed with a real injected rename failure in a later packet if stronger deterministic evidence is required.
- API/migration/settings summaries are classification summaries only; no semantic review is claimed.
- DB migration, event schema, Profile switch, and external service compatibility require manual review.
