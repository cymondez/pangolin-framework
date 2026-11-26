"""M0 engineering guard regressions; these are not A01-A95 runtime tests."""
import copy
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import m0
import product_version


class BaselineChecks(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.groups = m0.load(ROOT / "tests/contract/subtests.json")["groups"]
        cls.profiles = [m0.load(ROOT / f"tests/contract/profiles/{p}.json") for p in ("tenant-only", "organization-enabled", "replacement-contract")]

    def test_all_named_plans_and_fixture_references(self):
        self.assertEqual(m0.check_data(ROOT), {"groups":95,"named_subtests":532,"profiles":3,"fixture_files":4})

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
                    m0.check_profiles(self.groups, profiles)
        for profile_index, ident in [(0,"A92/catalog-removal-blocked"),(0,"A74/unsupported-kind-no-fallback"),(0,"A77/missing-scope"),(0,"A75/direct-application-entry"),(0,"A95/tenant-only-build-independent"),(1,"A01/cycle"),(2,"A88/stale-jwt-cache")]:
            with self.subTest(profile=profile_index, subtest=ident):
                profiles = copy.deepcopy(self.profiles)
                entry = next(e for e in profiles[profile_index]["subtests"] if e["id"] == ident)
                entry.update(applicability="not-applicable", status="not-applicable")
                with self.assertRaises(ValueError):
                    m0.check_profiles(self.groups, profiles)

    def test_api_signature_drift_is_detected(self):
        expected = m0.load(ROOT / "tests/contract/api.json")
        m0.check_api(expected, copy.deepcopy(expected))
        changed = copy.deepcopy(expected)
        changed["packages"].pop(next(iter(changed["packages"])))
        with self.assertRaises(ValueError):
            m0.check_api(expected, changed)

    def test_architecture_protects_domain_application_and_core(self):
        module = m0.MODULE
        m0.check_imports([{"ImportPath":module+"/kernel/module", "Imports":[module+"/kernel/di"]}, {"ImportPath":module+"/examples/bookstore/application", "Imports":[module+"/persistence/uow", module+"/persistence/database"]}])
        for path, imported in [("examples/bookstore/domain","net/http"),("examples/bookstore/domain","github.com/jackc/pgx/v5"),("examples/bookstore/application",module+"/persistence/postgres"),("examples/bookstore/application",module+"/persistence/mysql"),("examples/bookstore/application",module+"/persistence/sqlite"),("examples/bookstore/application","github.com/go-sql-driver/mysql"),("examples/bookstore/application","modernc.org/sqlite"),("kernel/di",module+"/modules/organization/contracts")]:
            with self.subTest(path=path, imported=imported):
                with self.assertRaises(ValueError):
                    m0.check_imports([{"ImportPath":module+"/"+path,"Imports":[imported]}])

    def test_configuration_retention_and_positive_limits(self):
        profile = m0.load(ROOT / "deploy/profiles/local.json")
        m0.check_config(profile)
        for section, key, value in [("uow","default_timeout","0s"),("uow","cleanup_timeout","unbounded"),("worker","lease","-1s"),("worker","concurrency",0),("worker","jitter_fraction",float("nan")),("events","inbox_retention","1h"),("idempotency","result_retention","1h")]:
            with self.subTest(section=section,key=key):
                changed = copy.deepcopy(profile)
                changed[section][key] = value
                with self.assertRaises(ValueError):
                    m0.check_config(changed)

    def test_concatenated_go_inventory(self):
        self.assertEqual(m0.json_stream(' {"a":1}\n {"b":2}\n'), [{"a":1},{"b":2}])
        with self.assertRaises(ValueError):
            m0.json_stream('{"a":')

    def test_same_commit_logs_and_required_commands_are_enforced(self):
        (ROOT / ".cache").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            path = Path(directory)
            self.assertTrue(path.resolve().is_relative_to((ROOT / ".cache").resolve()))
            (path / "00.log").write_text("fixture execution log", encoding="utf-8")
            hashed = m0.digest(path / "00.log")
            args = [["go","build","./..."],["go","vet","./..."],["go","run","./tools/check"],["python","-m","unittest"],["go","test","-json"],["go","run","./tools/check","-coverage","unit.out"],["go","run","./tools/check","-release"],["go","test","-race","./..."]]
            report = {"commit":"a"*40,"baseline":"v1.3","platform":"linux-amd64","working_tree_dirty":False,"status":"passed","runtime_acceptance":"planned","commands":[{"argv":a,"exit_code":1 if a[-1]=="-release" else 0,"log":"00.log","sha256":hashed} for a in args],"inputs":{"config":"digest"},"artifacts":{"00.log":hashed},"race":"passed"}
            m0.validate_report(report,"linux-amd64","a"*40,path)
            changes = {
                "wrong-commit": lambda r:r.update(commit="b"*40),
                "dirty": lambda r:r.update(working_tree_dirty=True),
                "no-inputs": lambda r:r.update(inputs={}),
                "no-artifacts": lambda r:r.update(artifacts={}),
                "wrong-platform": lambda r:r.update(platform="windows-amd64"),
                "missing-race-command": lambda r:r["commands"].pop(),
                "missing-build": lambda r:r["commands"].pop(0),
                "failed-command": lambda r:r["commands"][0].update(exit_code=1),
                "log-tamper": lambda r:r["artifacts"].update({"00.log":"0"*64}),
                "command-hash-tamper": lambda r:r["commands"][0].update(sha256="0"*64),
                "path-traversal": lambda r:r["artifacts"].update({"../00.log":hashed}),
            }
            for name, change in changes.items():
                with self.subTest(name=name):
                    edited=copy.deepcopy(report)
                    change(edited)
                    with self.assertRaises(ValueError):m0.validate_report(edited,"linux-amd64","a"*40,path)


if __name__ == "__main__":
    unittest.main()
