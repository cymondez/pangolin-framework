"""Baseline contract and engineering guard regressions; these are not A01-A95 runtime tests."""
import copy
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import contracts
import product_version
import test_runner


class BaselineChecks(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.groups = contracts.load(ROOT / "tests/contract/subtests.json")["groups"]
        cls.profiles = [contracts.load(ROOT / f"tests/contract/profiles/{p}.json") for p in ("tenant-only", "organization-enabled", "replacement-contract")]

    def test_all_named_plans_and_fixture_references(self):
        # The live tree carries the 9-requirement RT appendix (RT-001..RT-009);
        # the exact historical 95/532 plan stays enforced for M6-or-earlier scopes.
        self.assertEqual(contracts.check_data(ROOT), {"groups": 104, "named_subtests": 593, "profiles": 3, "fixture_files": 4})

    def test_product_version_rejects_milestone_and_module_drift(self):
        self.assertEqual(product_version.check(ROOT), "0.1.0")
        paths = ("VERSION", "examples/bookstore/bootstrap/run.go", "tests/fixtures/modules.json", "docs/site/package.json", "docs/site/package-lock.json", "internal/scaffold/cli.go")
        (ROOT / ".cache").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            root = Path(directory)
            for name in paths:
                target = root / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes((ROOT / name).read_bytes())
            self.assertEqual(product_version.check(root), "0.1.0")
            for name in paths:
                with self.subTest(path=name):
                    target = root / name
                    original = target.read_text(encoding="utf-8-sig")
                    target.write_text(original.replace("0.1.0", "0.2.0", 1), encoding="utf-8")
                    with self.assertRaises(ValueError):
                        product_version.check(root)
                    target.write_text(original, encoding="utf-8")

    def test_missing_duplicate_false_pass_and_na_are_rejected(self):
        mutations = {
            "missing": lambda p: p[0]["subtests"].pop(),
            "duplicate": lambda p: p[0]["subtests"].append(copy.deepcopy(p[0]["subtests"][0])),
            "fake-pass": lambda p: p[0]["subtests"][0].update(status="verified"),
            "wrong-requirement": lambda p: p[0]["subtests"][0].update(requirement="MOD-999"),
            "no-reason": lambda p: p[0]["subtests"][0].update(reason=""),
            "no-evidence": lambda p: p[0]["subtests"][0].update(evidence=""),
            "bad-applicability": lambda p: p[0]["subtests"][0].update(applicability="optional"),
            "wrong-baseline": lambda p: p[0].update(baseline="v0"),
            "profile-fake-pass": lambda p: p[0].update(status="verified"),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                profiles = copy.deepcopy(self.profiles)
                mutate(profiles)
                with self.assertRaises(ValueError):
                    contracts.check_profiles(self.groups, profiles)
        for profile_index, ident in [(0, "A92/catalog-removal-blocked"), (0, "A74/unsupported-kind-no-fallback"), (0, "A77/missing-scope"), (0, "A75/direct-application-entry"), (0, "A95/tenant-only-build-independent"), (1, "A01/cycle"), (2, "A88/stale-jwt-cache")]:
            with self.subTest(profile=profile_index, subtest=ident):
                profiles = copy.deepcopy(self.profiles)
                entry = next(e for e in profiles[profile_index]["subtests"] if e["id"] == ident)
                entry.update(applicability="not-applicable", status="not-applicable")
                with self.assertRaises(ValueError):
                    contracts.check_profiles(self.groups, profiles)

    def test_api_signature_drift_is_detected(self):
        expected = contracts.load(ROOT / "tests/contract/api.json")
        contracts.check_api(expected, copy.deepcopy(expected))
        changed = copy.deepcopy(expected)
        changed["packages"].pop(next(iter(changed["packages"])))
        with self.assertRaises(ValueError):
            contracts.check_api(expected, changed)

    def test_architecture_protects_domain_application_and_core(self):
        module = contracts.MODULE
        contracts.check_imports([{"ImportPath": module + "/kernel/module", "Imports": [module + "/kernel/di"]}, {"ImportPath": module + "/examples/bookstore/application", "Imports": [module + "/persistence/uow", module + "/persistence/database"]}])
        for path, imported in [("examples/bookstore/domain", "net/http"), ("examples/bookstore/domain", "github.com/jackc/pgx/v5"), ("examples/bookstore/application", module + "/persistence/postgres"), ("examples/bookstore/application", module + "/persistence/mysql"), ("examples/bookstore/application", module + "/persistence/sqlite"), ("examples/bookstore/application", "github.com/go-sql-driver/mysql"), ("examples/bookstore/application", "modernc.org/sqlite"), ("kernel/di", module + "/modules/organization/contracts")]:
            with self.subTest(path=path, imported=imported):
                with self.assertRaises(ValueError):
                    contracts.check_imports([{"ImportPath": module + "/" + path, "Imports": [imported]}])

    def test_configuration_retention_and_positive_limits(self):
        profile = contracts.load(ROOT / "deploy/profiles/local.json")
        contracts.check_config(profile)
        for section, key, value in [("uow", "default_timeout", "0s"), ("uow", "cleanup_timeout", "unbounded"), ("worker", "lease", "-1s"), ("worker", "concurrency", 0), ("worker", "jitter_fraction", float("nan")), ("events", "inbox_retention", "1h"), ("idempotency", "result_retention", "1h")]:
            with self.subTest(section=section, key=key):
                changed = copy.deepcopy(profile)
                changed[section][key] = value
                with self.assertRaises(ValueError):
                    contracts.check_config(changed)

    def test_concatenated_go_inventory(self):
        self.assertEqual(contracts.json_stream(' {"a":1}\n {"b":2}\n'), [{"a": 1}, {"b": 2}])
        with self.assertRaises(ValueError):
            contracts.json_stream('{"a":')

    def test_coverage_fails_before_expensive_non_profile_checks(self):
        source = (ROOT / "scripts/test_runner.py").read_text(encoding="utf-8-sig")
        coverage = source.index('counts = execute(["go", "run", "./tools/check", "-coverage"')
        expensive = (
            'execute(["go", "mod", "verify"], "go-module-verify")',
            'execute([sys.executable, "scripts/templates_verify.py", "compose"',
            'race = execute(["go", "test"',
        )
        for command in expensive:
            with self.subTest(command=command):
                self.assertGreater(source.index(command), coverage)

    def test_same_commit_logs_and_required_commands_are_enforced(self):
        (ROOT / ".cache").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            path = Path(directory)
            self.assertTrue(path.resolve().is_relative_to((ROOT / ".cache").resolve()))
            names = ["unit.out", "integration.out", "e2e.out", "merged.out", "coverage.json", "coverage.html", "go-list.json", "exclusions.json", "dependencies.json", "00.log"]
            for name in names:
                (path / name).write_text("fixture execution log\n", encoding="utf-8")
            artifacts = {name: contracts.digest(path / name) for name in names}
            hashed = artifacts["00.log"]
            cmd_names = ("inventory", "build", "vet", "go-module-verify", "unit", "m0-regression", "m1-regression", "integration", "e2e", "coverage", "generation", "race")
            commands = [{"name": name, "exit_code": 0, "log": "00.log", "sha256": hashed} for name in cmd_names]
            e2e = {k: True for k in ("producer_consumer_processes", "signed_jwt", "commit_before_2xx", "cross_tenant_rejected", "crash_after_broker_ack")}
            e2e.update(outbox_attempts=2, inbox_processed=1)
            report = {
                "commit": "a" * 40,
                "baseline": "v1.3",
                "platform": {"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1"},
                "working_tree_dirty": False,
                "status": "passed",
                "commands": commands,
                "e2e": e2e,
                "artifacts": artifacts,
                "tests": list(contracts.load(ROOT / "tests/contract/m1.json")["go_tests"].values()),
            }
            test_runner.validate_report(report, "a" * 40, path)
            changes = {
                "wrong-commit": lambda r: r.update(commit="b" * 40),
                "dirty": lambda r: r.update(working_tree_dirty=True),
                "no-artifacts": lambda r: r.update(artifacts={}),
                "missing-cgo-on-linux": lambda r: r["platform"].update(CGO_ENABLED="0"),
                "missing-race-command": lambda r: r["commands"].pop(),
                "missing-build": lambda r: r["commands"].pop(1),
                "failed-command": lambda r: r["commands"][0].update(exit_code=1),
                "log-tamper": lambda r: r["artifacts"].update({"00.log": "0" * 64}),
                "command-hash-tamper": lambda r: r["commands"][0].update(sha256="0" * 64),
                "path-traversal": lambda r: r["artifacts"].update({"../00.log": hashed}),
            }
            for name, change in changes.items():
                with self.subTest(name=name):
                    edited = copy.deepcopy(report)
                    change(edited)
                    with self.assertRaises(ValueError):
                        test_runner.validate_report(edited, "a" * 40, path)


if __name__ == "__main__":
    unittest.main()
