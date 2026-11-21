"""Derive named runtime test plans from the authoritative A01-A95 table.

This generates plans, not executable runtime tests or passing test results.
Review scenario edits together with the corresponding specification row.
"""
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
# Each name selects one stimulus already present in the authoritative row.
SCENARIOS = {
  1: "duplicate-name missing-dependency incompatible-version cycle stable-layer-order",
  2: "phase-order partial-start-failure reverse-cleanup no-ready-on-failure",
  3: "required-two-repositories-outer-rollback",
  4: "handler-chain-commit handler-error outbox-error audit-error",
  5: "precommit-failure explicit-rollback-response",
  6: "two-worker-barrier crash-before-commit retry-after-crash",
  7: "replay-success crash-after-commit-before-response",
  8: "http-tenant-hint job-tenant-tamper event-tenant-tamper untrusted-producer",
  9: "interleaved-auto-queries connection-reuse explicit-nontransactional-rejected",
 10: "concurrent-version-conflict",
 11: "lease-takeover stale-success-update stale-failure-update",
 12: "valid-token expired-token forged-token issuer audience algorithm jwks-rotation",
 13: "allow explicit-user-deny role-tenant-matrix feature-not-permission direct-use-case",
 14: "success-after-commit precommit-error explicit-rollback unknown-no-2xx",
 15: "active-http active-worker drain-timeout reverse-stop close-once",
 16: "old-schema optional-field unknown-schema breaking-change",
 17: "request-isolation current-active root-closed required-view-closed parent-still-active",
 18: "zero-options negative-timeout unknown-enum nil-context nil-callback nil-hook missing-defaults",
 19: "http-command http-query job-command job-query consumer-command consumer-query",
 20: "no-scope read-only nontransactional closed-scope writable-control",
 21: "compatible-grid access-conflict transaction-conflict isolation-conflict identity-conflict caught-conflict",
 22: "caught-inner-error explicit-rollback-only",
 23: "independent-resource suspended-parent resumed-parent",
 24: "child-commit-parent-rollback",
 25: "caught-child-error propagated-child-error",
 26: "depth-boundary depth-exceeded pool-exhaustion parent-lock-wait",
 27: "unknown-propagation no-savepoint-api no-suppress-api",
 28: "pool-deadline begin-deadline sql-deadline handler-deadline commit-deadline longer-child shorter-child",
 29: "cancel-before-commit independent-cleanup-context",
 30: "overlapping-sql open-rows-busy rows-delayed-error race-free",
 31: "flush-then-rollback no-event-drain no-completion-hook immediate-sql-adapter",
 32: "single-root-commit single-root-rollback no-public-commit-handle",
 33: "disconnect-before-delivery disconnect-after-db-commit reconcile-same-key",
 34: "canceled-cleanup rollback-failure original-cause broken-connection unknown-preserved",
 35: "callback-panic handler-panic panic-value-preserved no-resource-leak",
 36: "root-and-required-success rolled-back-no-completed unknown-no-completed requires-new-hooks",
 37: "hook-error hook-panic cooperative-hook-timeout later-hooks-run",
 38: "committed failed panic unknown disposed-once observer-error-isolated",
 39: "cancel-after-commit response-lost-after-commit query-result",
 40: "serialization-failure deadlock commit-unknown explicit-upper-retry",
 41: "second-resource-key-rejected",
 42: "sentinel-codes errors-is errors-as http-mapping detail-redaction unknown-retry-policy",
 43: "read-only-child-query read-only-child-write-rejected",
 44: "requires-new-tenant-switch requires-new-principal-switch",
 45: "allowed-import forbidden-import-regression",
 46: "singleton transient operation missing-provider cycle duplicate-provider explicit-replace sealed-registry factory-failure scoped-cleanup captive-scope",
 47: "four-layer-precedence missing-timeout invalid-config sealed-config secret-redaction",
 48: "invalid-identity entry-denied malformed-dto oversized-body valid-request resource-policy problem-details",
 49: "two-short-scopes no-transaction-for-connection-lifetime",
 50: "commit rollback no-scope outbox-disabled",
 51: "producer-restart full-envelope-replay scope-preserved",
 52: "durable-ack unroutable rejected timed-out broker-restart",
 53: "persist-before-ack database-failure receiver-crash-before-persist receiver-crash-after-persist identical-duplicate conflicting-duplicate",
 54: "external-success-local-crash stable-provider-key no-exactly-once-claim",
 55: "priority-registration-order fifo-new-events event-limit depth-limit cyclic-chain",
 56: "missing-tenant missing-sql-filter cross-tenant-read cross-tenant-write cross-tenant-soft-delete role-attributes",
 57: "cross-tenant-fk same-tenant-fk existence-redaction",
 58: "ordinary-override-denied host-policy-allow host-audit no-global-bypass",
 59: "empty-database forward-upgrade concurrent-deploy incompatible-schema api-does-not-migrate",
 60: "same-key-same-body same-key-different-body holder-rollback wait-timeout",
 61: "authorized-success other-principal-denied pending-not-failed retention-boundary expired-guarantee",
 62: "success-required-audit auth-denial-log business-rollback-log audit-write-failure telemetry-failure",
 63: "broker-offline capacity-limit retry-limit dead-replay recovery-drain tenant-load-competition",
 64: "bounded-retry cancellation stale-lease manual-replay-audit tenant-tamper handler-idempotency",
 65: "tenant-cache-isolation setting-precedence feature-permission invalidation-failure ttl-version-convergence",
 66: "trace-correlation alerting liveness required-dependency-unready optional-broker-outage redaction",
 67: "new module migrate-create migrate-status doctor openapi help invalid-arguments existing-files reentry generated-build-test",
 68: "below-threshold exact-threshold aggregate-masks-critical-package missing-profile untested-package duplicate-block-union",
 69: "required-case-fails required-case-skipped race-fails static-analysis-fails dependency-license-fails coverage-cannot-mask-failure",
 70: "missing-release-item unknown-requirement duplicate-delivery template-runtime-version",
 71: "new-increment previous-increment-regression same-commit-evidence",
 72: "clean-rebuild forward-upgrade backup-restore load-baseline security-review",
 73: "full-tuple-identity missing-field tenant-id-mismatch parent-not-union",
 74: "tenant-only organization-enabled replacement-provider missing-required-provider duplicate-kind unsupported-kind-no-fallback",
 75: "header-route-conflict dto-owner-tamper unauthorized-scope provider-tenant-mismatch provider-request-mismatch direct-application-entry",
 76: "required-switch requires-new-switch new-authorized-top-level",
 77: "missing-owner-filter forged-owner parent-cannot-read-child org-cannot-use-tenant-only missing-scope pool-reuse",
 78: "unknown-owner cross-tenant-fk cross-owner-fk same-owner-control neutral-catalog-dependency",
 79: "restart-replay scope-tamper revoked-user current-service-grant missing-org-scope unavailable-provider legacy-tenant-upcast",
 80: "scope-cache-isolation scoped-invalidation setting-inheritance parent-security-restriction audit-owner",
 81: "scope-specific-key replay-same-scope cross-scope-result-denied revoked-replay-denied committed-effect-preserved",
 82: "tenant-only-build no-core-organization-import single-optional-module replacement-assembly",
 83: "standalone single-parent multiple-parent-rejected cross-tenant-rejected owner-stable-on-transfer",
 84: "multi-organization-membership independent-roles scoped-revocation tenant-membership-revoked",
 85: "enterprise-directory governance-policy enterprise-owner-no-org-content derived-member-no-org-permission",
 86: "role-policy-grid explicit-deny parent-allow-no-membership feature-no-grant unauthorized-self-elevation audited-owner-join",
 87: "atomic-success membership-write-failure catalog-write-failure audit-write-failure outbox-write-failure revision-consistency",
 88: "revocation-between-bind-and-admission new-operation-after-revocation stale-jwt-cache admitted-operation-snapshot",
 89: "unauthorized-transfer cross-tenant-transfer nonempty-parent-delete valid-same-tenant-transfer stable-owner-and-new-ancestry",
 90: "last-owner-removal-denied tenant-revocation-allowed ownerless-denied audited-recovery",
 91: "provider-export-import stable-owner-id membership-state-revision preserved-event-replay negative-authorization-parity",
 92: "catalog-removal-blocked business-owner-removal-blocked pending-job-removal-blocked pending-event-removal-blocked required-capability-blocked no-tenant-downgrade",
 93: "tenant-owner-backfill interrupted-backfill interrupted-policy-switch forward-resume no-permissive-policy-union",
 94: "default-tenant-template explicit-organization-template complete-module-migrations no-half-enterprise-template",
 95: "organization-contract-fails applicable-case-skipped critical-package-below-threshold tenant-only-build-independent",
}


def generate(root=ROOT):
    spec = (root / "design/panGOlin-framework_實作規範書.md").read_text(encoding="utf-8-sig")
    base = json.loads((root / "tests/contract/cases.json").read_text())
    rows = {}
    for line in spec.splitlines():
        if re.match(r"^\| A\d{2} \|", line):
            columns = [column.strip() for column in line.strip("|").split("|")]
            if len(columns) != 5:
                raise ValueError("unexpected acceptance table shape: " + line)
            rows[columns[0]] = columns
    groups = []
    for case in base["cases"]:
        number = int(case["id"][1:])
        columns = rows[case["id"]]
        group = dict(case, setup=columns[2], stimulus=columns[3], expected=columns[4],
                     source_sha256=hashlib.sha256("|".join(columns).encode()).hexdigest(),
                     fixture="tests/fixtures/manifest.json", subtests=[])
        for name in SCENARIOS[number].split():
            group["subtests"].append({"id": case["id"] + "/" + name,
                "stimulus_variant": name, "assertion_ref": case["id"] + ".expected",
                "status": "planned", "evidence": "artifacts/{commit}/{profile}/" + case["id"] + "/" + name + "/"})
        groups.append(group)
    matrix = {"baseline": "v1.3", "authority": "design/panGOlin-framework_實作規範書.md#s-13.2", "groups": groups}
    (root / "tests/contract/subtests.json").write_text(json.dumps(matrix, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    for profile in ("tenant-only", "organization-enabled", "replacement-contract"):
        entries = []
        for group in groups:
            for subtest in group["subtests"]:
                na = profile == "tenant-only" and (83 <= int(group["id"][1:]) <= 91 or group["id"] == "A93" or group["id"] == "A76" or group["id"] in ("A77", "A78", "A80", "A81") or group["id"] == "A94" and subtest["stimulus_variant"] != "default-tenant-template")
                if profile == "tenant-only":
                    if group["id"] == "A77" and subtest["stimulus_variant"] in ("missing-scope", "pool-reuse"):
                        na = False
                    if group["id"] == "A74" and subtest["stimulus_variant"] in ("organization-enabled", "replacement-provider") or group["id"] == "A82" and subtest["stimulus_variant"] == "replacement-assembly":
                        na = True
                    if group["id"] == "A79" and subtest["stimulus_variant"] in ("restart-replay", "current-service-grant"):
                        na = True
                # Missing/unsupported-provider and downgrade negatives stay required.
                entries.append({"id": subtest["id"], "requirement": group["requirement"],
                    "applicability": "not-applicable" if na else "required",
                    "reason": "§13.4: no organization provider/schema or organization-owned data in this assembly; applicable missing-kind/capability/downgrade negatives remain in A74/A75/A79/A82/A92/A95" if na else "§13.4: framework/core contract or applicable negative scenario",
                    "status": "not-applicable" if na else "planned", "evidence": subtest["evidence"]})
        output = {"baseline": "v1.3", "profile": profile, "status": "planned", "subtests": entries}
        (root / f"tests/contract/profiles/{profile}.json").write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return len(groups), sum(len(group["subtests"]) for group in groups)


if __name__ == "__main__":
    print("Generated planned groups/subtests:", generate())
