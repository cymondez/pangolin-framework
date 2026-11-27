"""Regression guards for M1 engineering gates, never substitute for real services."""
import copy
import json
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import contracts
import dependencies
import test_runner


class EngineeringGates(unittest.TestCase):
    def test_framework_never_imports_examples(self):
        for area in ("kernel/runtime", "persistence/postgres", "security/authn", "events/nats", "transport/http"):
            with self.subTest(area=area), self.assertRaisesRegex(ValueError, "[Ff]ramework.*example"):
                contracts.check_imports([{"ImportPath": contracts.MODULE + "/" + area, "Imports": [contracts.MODULE + "/examples/bookstore/domain"]}])
        contracts.check_imports([{"ImportPath": contracts.MODULE + "/examples/bookstore/bootstrap", "Imports": [contracts.MODULE + "/kernel/runtime"]}])
        with self.assertRaises(ValueError):
            contracts.check_imports([{"ImportPath": contracts.MODULE + "/examples/bookstore/application", "Imports": [contracts.MODULE + "/examples/bookstore/transport/http"]}])
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            root = Path(directory)
            (root / "go.mod").write_text("module " + contracts.MODULE + "\n\ngo 1.27.0\n")
            (root / "kernel/runtime").mkdir(parents=True)
            (root / "examples/bookstore/domain").mkdir(parents=True)
            (root / "examples/bookstore/domain/book.go").write_text("package domain\ntype Book struct{}\n")
            (root / "kernel/runtime/host.go").write_text('package runtime\nimport _ "' + contracts.MODULE + '/examples/bookstore/domain"\n')
            with self.assertRaisesRegex(ValueError, "framework imports example"):
                contracts.check_imports(contracts.json_stream(contracts.command(["go", "list", "-json", "./..."], root)))

    def test_A45_real_import_graph_injection(self):
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            root = Path(directory)
            (root / "go.mod").write_text("module " + contracts.MODULE + "/fixture\n\ngo 1.27.0\n")
            domain = root / "domain"
            domain.mkdir()
            source = domain / "domain.go"
            for imported, allowed in [("context", True), ("net/http", False), ("context", True)]:
                source.write_text('package domain\nimport _ "' + imported + '"\n')
                packages = contracts.json_stream(contracts.command(["go", "list", "-json", "./..."], root))
                if allowed:
                    contracts.check_imports(packages)
                else:
                    with self.assertRaises(ValueError):
                        contracts.check_imports(packages)
            infrastructure = root / "infrastructure"
            infrastructure.mkdir()
            (infrastructure / "store.go").write_text("package infrastructure\ntype Store struct{}\n")
            application = root / "application"
            application.mkdir()
            (application / "app.go").write_text('package application\nimport _ "' + contracts.MODULE + '/fixture/infrastructure"\n')
            with self.assertRaises(ValueError):
                contracts.check_imports(contracts.json_stream(contracts.command(["go", "list", "-json", "./..."], root)))
            for imported in (contracts.MODULE + "/examples/bookstore/infrastructure", contracts.MODULE + "/events/nats"):
                with self.subTest(imported=imported), self.assertRaises(ValueError):
                    contracts.check_imports([{"ImportPath": contracts.MODULE + "/examples/bookstore/application", "Imports": [imported]}])

    def test_A69_failure_skip_and_missing_case(self):
        event = {"Action": "pass", "Package": "pkg", "Test": "TestRequired"}
        self.assertIn("pkg:TestRequired", test_runner.validate_tests(json.dumps(event), ["pkg:TestRequired"]))
        for action in ("fail", "skip"):
            with self.subTest(action=action), self.assertRaises(ValueError):
                test_runner.validate_tests(json.dumps(dict(event, Action=action)))
        for text in ("", json.dumps(event)):
            with self.subTest(text=text), self.assertRaises(ValueError):
                test_runner.validate_tests(text, ["pkg:TestMissing"])
        test_runner.validate_python_output("Ran 6 tests in 0.1s\n\nOK\n")
        for text in ("Ran 0 tests in 0s\n\nOK\n", "Ran 6 tests in 0s\n\nOK (skipped=1)\n", ""):
            with self.subTest(text=text), self.assertRaises(ValueError):
                test_runner.validate_python_output(text)

    def test_A69_independent_gates(self):
        test_runner.check_module_docs(contracts.load(ROOT / "tests/contract/m1.json"))
        with self.assertRaises(ValueError):
            test_runner.check_module_docs({"module_docs": ["missing-module/README.md"]})
        names = ["inventory", "build", "vet", "go-module-verify", "unit", "m0-regression", "m1-regression", "integration", "e2e", "coverage", "generation", "race"]
        commands = [{"name": name, "exit_code": 0} for name in names]
        test_runner.validate_commands(commands, True)
        for name in names:
            altered = copy.deepcopy(commands)
            next(c for c in altered if c["name"] == name)["exit_code"] = 1
            with self.subTest(name=name), self.assertRaises(ValueError):
                test_runner.validate_commands(altered, True)
        with self.assertRaises(ValueError):
            test_runner.validate_commands(commands[:-1], True)

    def test_A69_dependency_license_mutation(self):
        reviewed = contracts.load(ROOT / "deploy/dependencies.json")
        self.assertEqual(len(reviewed["modules"]), dependencies.check()["locked_modules"])
        actual = dependencies.inventory()
        self.assertTrue(all(x["sum"] and x["license_sha256"] for x in actual))
        for key in ("version", "sum", "license_sha256", "license"):
            altered = copy.deepcopy(reviewed)
            altered["modules"][0][key] = "unreviewed"
            with self.subTest(key=key), self.assertRaises(ValueError):
                dependencies.validate(altered, actual)

    def test_A71_coverage_union_metadata_and_previous_case(self):
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            root = Path(directory)
            a = root / "a.out"
            b = root / "b.out"
            merged = root / "merged.out"
            a.write_text("mode: atomic\nx/a.go:1.1,2.1 2 1\nx/a.go:3.1,4.1 8 0\n")
            b.write_text("mode: atomic\nx/a.go:1.1,2.1 2 2\nx/a.go:3.1,4.1 8 1\n")
            test_runner.merge_profiles([a, b], merged)
            self.assertEqual(len(merged.read_text().splitlines()), 3)
            b.write_text("mode: atomic\nx/a.go:1.1,2.1 3 1\n")
            with self.assertRaises(ValueError):
                test_runner.merge_profiles([a, b], merged)
            b.write_text("")
            with self.assertRaises(ValueError):
                test_runner.merge_profiles([a, b], merged)
        previous = {"Action": "pass", "Package": "old", "Test": "TestPrevious"}
        new = {"Action": "pass", "Package": "new", "Test": "TestNew"}
        test_runner.validate_tests(json.dumps(previous) + "\n" + json.dumps(new), ["old:TestPrevious", "new:TestNew"])
        with self.assertRaises(ValueError):
            test_runner.validate_tests(json.dumps(new), ["old:TestPrevious", "new:TestNew"])

    def test_A71_same_commit_and_artifact_integrity(self):
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            root = Path(directory)
            names = ["unit.out", "integration.out", "e2e.out", "merged.out", "coverage.json", "coverage.html", "go-list.json", "exclusions.json", "dependencies.json", "command.log"]
            for name in names:
                (root / name).write_text("fixture evidence\n")
            artifacts = {name: contracts.digest(root / name) for name in names}
            commands = [{"name": name, "exit_code": 0, "log": "command.log", "sha256": artifacts["command.log"]} for name in ("inventory", "build", "vet", "go-module-verify", "unit", "m0-regression", "m1-regression", "integration", "e2e", "coverage", "generation")]
            e2e = {k: True for k in ("producer_consumer_processes", "signed_jwt", "commit_before_2xx", "cross_tenant_rejected", "crash_after_broker_ack")}
            e2e.update(outbox_attempts=2, inbox_processed=1)
            report = {"commit": "a" * 40, "baseline": "v1.3", "working_tree_dirty": False, "status": "passed", "platform": {"GOOS": "windows"}, "commands": commands, "e2e": e2e, "artifacts": artifacts, "tests": list(contracts.load(ROOT / "tests/contract/m1.json")["go_tests"].values())}
            test_runner.validate_report(report, "a" * 40, root)
            for key, value in (("commit", "b" * 40), ("working_tree_dirty", True), ("status", "failed"), ("baseline", "wrong"), ("tests", [])):
                changed = copy.deepcopy(report)
                changed[key] = value
                with self.subTest(key=key), self.assertRaises(ValueError):
                    test_runner.validate_report(changed, "a" * 40, root)
            (root / "unit.out").write_text("tampered")
            with self.assertRaises(ValueError):
                test_runner.validate_report(report, "a" * 40, root)


if __name__ == "__main__":
    unittest.main()
