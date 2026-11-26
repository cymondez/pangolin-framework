"""M6 release gate regressions: fail-closed manifest, drills and closure evidence.

These tests reject phantom evidence, unknown/duplicate/missing release items,
version drift, required skips, contract failures and sub-threshold critical
coverage. They never assert that the release itself is Verified; official
status stays In progress until same-commit gate evidence passes.
"""
import copy
import json
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import m0
import m1
import m6
import release_manifest


def drill_fixture(commit="a" * 40, source="b" * 64, platform="windows-amd64", project="pangolin-m6-test", dirty=False):
    """A complete drill report matching scripts/m6_drills.py's emitted shape."""
    data = {
        "interface": "python scripts/m6_drills.py --output <dir> --project <project>",
        "baseline": "v1.3", "product_version": "0.1.0",
        "commit": commit, "source_sha256": source,
        "working_tree_dirty": dirty, "evidence_class": "development-only" if dirty else "evidence-ready",
        "platform": platform,
        "platform_details": {"GOOS": platform.split("-")[0], "GOARCH": "amd64", "GOVERSION": "go1.27.4", "CGO_ENABLED": "1"},
        "compose": {"project": project.removesuffix("-drills"), "files": ["deploy/compose/m1.yaml"]},
        "project": project, "status": "passed", "error": None,
        "commands": [{"argv": ["go", "version"], "exit_code": 0, "log": "report-00.log", "sha256": ""}],
        "artifacts": {},
        "drills": {
            "clean-rebuild": {"status": "passed", "commands": [], "checks": {
                                  "clean_source": True, "clean_database": True, "clean_build": True, "tests_passed": True},
                              "exported_files": 100, "database": "m6_rebuild_x"},
            "forward-upgrade": {"status": "passed", "commands": [], "checks": {
                                    "seeded_tenant_rows": 2, "owner_preserved": True, "force_rls": True, "permissive_policy_left": False},
                                "database": "m6_upgrade_x", "upgrade_seconds": 3.2},
            "backup-restore": {"status": "passed", "commands": [], "checks": {
                                   "restore_verified": True, "row_counts_match": True, "checksums_match": True, "independent_connection": True},
                               "database": "m6_backup_x", "restore_database": "m6_restore_x",
                               "pg": {"mode": "host", "dump_exit": 0, "restore_exit": 0, "dump_sha256": "0" * 64},
                               "rpo": {"rows_missing": 0, "window_seconds": 1.5}, "rto": {"total_seconds": 9.0}},
            "load-baseline": {"status": "passed", "commands": [], "checks": {
                                  "hardware": "test-host", "go_version": "go1.27.4", "database": "PostgreSQL 18.7",
                                  "broker": "NATS 2.15.3", "pool_size": 8, "load_count": 12, "concurrency": 16,
                                  "p95_ms": 30.1, "p99_ms": 30.1, "methodology": "fixed 12-write load with wall-clock sampling",
                                  "samples": [11.1, 12.0, 12.5, 13.0, 14.2, 15.0, 16.3, 17.0, 18.4, 20.0, 22.5, 30.1]},
                              "database": "m6_load_x", "duration_seconds": 31.25, "ops_per_second": 16.0},
            "security-review": {"status": "passed", "commands": [], "checks": {
                                    "categories": list(release_manifest.THREAT_CATEGORIES), "unresolved_high_risk": 0,
                                    "controls": [{"category": category, "attack_surface": "surface", "abuse_path": "path",
                                                  "control": "control", "test": "package:TestX", "residual_risk": "accepted"}
                                                 for category in release_manifest.THREAT_CATEGORIES]},
                                "executable_tests": ["github.com/cymondez/pangolin-framework/security/authn:TestX"],
                                "review_document": "docs/security/release-review.md", "database": "m6_security_x"},
        },
    }
    for entry in data["drills"].values():
        entry["commands"].append({"argv": ["pg_dump", "--version"], "exit_code": 0, "log": "cmd-00.log", "sha256": ""})
    return data


def write_drills(directory, data):
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    logs = [data["commands"][0]["log"]]
    for entry in data["drills"].values():
        logs.extend(command["log"] for command in entry["commands"])
    for name in logs:
        log = directory / name
        log.write_text("drill command output\n", encoding="utf-8")
    data["commands"][0]["sha256"] = m0.digest(directory / data["commands"][0]["log"])
    for entry in data["drills"].values():
        for command in entry["commands"]:
            command["sha256"] = m0.digest(directory / command["log"])
    for name in ("clean-rebuild.json", "upgrade-drill.json", "backup-restore.json", "load-baseline.json", "security-drill.json"):
        (directory / name).write_text(json.dumps({"fixture": True}, indent=2) + "\n", encoding="utf-8")
    data["artifacts"] = {name: m0.digest(directory / name) for name in
                         ("clean-rebuild.json", "upgrade-drill.json", "backup-restore.json", "load-baseline.json", "security-drill.json")}
    (directory / "release-drills.json").write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
    return directory


class ReleaseGates(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.manifest = release_manifest.load_manifest()
        cls.groups = m0.load(ROOT / "tests/contract/subtests.json")["groups"]
        cls.mappings = release_manifest.load_mappings()

    # A70/missing-release-item
    def test_A70_missing_release_item(self):
        mutations = {
            "asset": lambda m: m["assets"].remove("docs/release-notes/0.1.0.md"),
            "delivery": lambda m: m["deliveries"].pop("REL-003"),
            "drill": lambda m: m["drills"].remove("backup-restore"),
            "profile": lambda m: m["profiles"].pop(),
            "schema": lambda m: m["event_schemas"]["entries"].pop(),
            "gate": lambda m: m["gates"].remove("linux-amd64"),
        }
        for name, mutate in mutations.items():
            with self.subTest(item=name):
                manifest = copy.deepcopy(self.manifest)
                mutate(manifest)
                with self.assertRaises(ValueError):
                    release_manifest.check_manifest(manifest)
        missing = [path for path in self.manifest["assets"] if not (ROOT / path).is_file()]
        if missing:
            with self.assertRaises(ValueError) as caught:
                release_manifest.check_assets(self.manifest)
            for path in missing:
                self.assertIn(path, str(caught.exception))

    # A70/unknown-requirement
    def test_A70_unknown_requirement(self):
        manifest = copy.deepcopy(self.manifest)
        manifest["deliveries"]["REL-096"] = manifest["deliveries"].pop("REL-003")
        with self.assertRaisesRegex(ValueError, "unknown requirement"):
            release_manifest.check_manifest(manifest)
        mappings = copy.deepcopy(self.mappings)
        mappings[6]["python_tests"]["A99/phantom"] = "test_gate.ReleaseGates.test_phantom"
        with self.assertRaisesRegex(ValueError, "unknown subtest"):
            release_manifest.check_closure(mappings, self.groups)
        mapping = copy.deepcopy(m0.load(ROOT / "tests/contract/m6.json"))
        mapping["drills"]["A72/clean-rebuild-x"] = "clean-rebuild"
        with self.assertRaises(ValueError):
            m6.validate_mapping(mapping)

    # A70/duplicate-delivery
    def test_A70_duplicate_delivery(self):
        manifest = copy.deepcopy(self.manifest)
        manifest["deliveries"]["REL-004"]["case"] = "A70"
        with self.assertRaisesRegex(ValueError, "duplicate delivery"):
            release_manifest.check_manifest(manifest)
        mappings = copy.deepcopy(self.mappings)
        stolen = next(iter(mappings[6]["python_tests"]))
        mappings[5]["unit_tests"][stolen] = "some/package:TestStolen"
        with self.assertRaisesRegex(ValueError, "duplicate subtest"):
            release_manifest.check_closure(mappings, self.groups)

    # A70/template-runtime-version
    def test_A70_template_runtime_version(self):
        versions = release_manifest.check_versions(self.manifest, ROOT)
        self.assertEqual(versions, {"runtime": "0.1.0", "cli": "0.1.0", "template": "0.1.0", "template_runtime_require": "v0.1.0"})
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            root = Path(directory)
            for name in ("VERSION", "internal/scaffold/cli.go", "examples/bookstore/bootstrap/run.go", "tests/fixtures/modules.json"):
                target = root / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes((ROOT / name).read_bytes())
            template = root / "internal/scaffold/assets/common/go.mod.tmpl"
            template.parent.mkdir(parents=True, exist_ok=True)
            template.write_bytes((ROOT / "internal/scaffold/assets/common/go.mod.tmpl").read_bytes())
            originals = {name: (root / name).read_bytes() for name in ("VERSION", "internal/scaffold/cli.go")}
            originals["template"] = template.read_bytes()
            release_manifest.check_versions(self.manifest, root)

            def drift_template():
                text = template.read_text(encoding="utf-8-sig")
                template.write_text(text.replace("require github.com/cymondez/pangolin-framework v0.1.0", "require github.com/cymondez/pangolin-framework v0.2.0", 1), encoding="utf-8")

            for name, mutate in {
                "template": drift_template,
                "runtime": lambda: (root / "VERSION").write_text("0.2.0", encoding="utf-8"),
                "cli": lambda: (root / "internal/scaffold/cli.go").write_text((root / "internal/scaffold/cli.go").read_text(encoding="utf-8-sig").replace('"0.1.0"', '"0.2.0"', 1), encoding="utf-8"),
            }.items():
                with self.subTest(drift=name):
                    mutate()
                    with self.assertRaises(ValueError):
                        release_manifest.check_versions(self.manifest, root)
                    (root / "VERSION").write_bytes(originals["VERSION"])
                    (root / "internal/scaffold/cli.go").write_bytes(originals["internal/scaffold/cli.go"])
                    template.write_bytes(originals["template"])
        # Wire schema versions are deliberately independent of the product version.
        for entry in self.manifest["event_schemas"]["entries"]:
            self.assertNotEqual(str(entry["schema_version"]), self.manifest["product_version"])
            self.assertGreaterEqual(int(entry["schema_version"]), 1)

    # A95/organization-contract-fails
    def test_A95_organization_contract_fails(self):
        report = {"tests": sorted({name for mapping in self.mappings.values() for name in mapping.get("unit_tests", {}).values()} | set(self.mappings[1].get("go_tests", {}).values())),
                  "integration_tests": sorted({name for mapping in self.mappings.values() for name in mapping.get("integration_tests", {}).values()})}
        release_manifest.check_report_closure(report, self.mappings, self.groups)
        organization = sorted(name for name in report["integration_tests"] if "/tests/m3:" in name)[:1]
        self.assertTrue(organization)
        broken = copy.deepcopy(report)
        broken["integration_tests"] = [name for name in broken["integration_tests"] if name != organization[0]]
        with self.assertRaisesRegex(ValueError, "missing required case"):
            release_manifest.check_report_closure(broken, self.mappings, self.groups)

    # A95/applicable-case-skipped
    def test_A95_applicable_case_skipped(self):
        log = "test_example (test_gate.ReleaseGates.test_example) ... ok\n\nRan 1 test in 0.001s\n\nOK (skipped=1)\n"
        with self.assertRaisesRegex(ValueError, "skip"):
            m1.validate_python_output(log)
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            data = drill_fixture()
            write_drills(directory, data)
            release_manifest.validate_drills(data, directory, "a" * 40, "b" * 64, "windows-amd64", host_run=False)
            data["drills"]["backup-restore"]["status"] = "skipped"
            with self.assertRaisesRegex(ValueError, "skip|failed"):
                release_manifest.validate_drills(data, directory, "a" * 40, "b" * 64, "windows-amd64", host_run=False)
        mappings = copy.deepcopy(self.mappings)
        mappings[6]["python_tests"].pop(next(iter(mappings[6]["python_tests"])))
        with self.assertRaisesRegex(ValueError, "missing subtest"):
            release_manifest.check_closure(mappings, self.groups)

    # A95/critical-package-below-threshold
    def test_A95_critical_package_below_threshold(self):
        module = m0.MODULE
        # One package under every critical prefix, each at exactly the raw-count
        # threshold: covered*100 == total*90.
        critical = {prefix: module + "/" + prefix + "/fixture" for prefix in self.manifest["critical_packages"]}
        base = {package: {"covered_statements": 90, "total_statements": 100} for package in critical.values()}
        passing = {"overall": {"covered_statements": 990, "total_statements": 1100}, "packages": dict(base)}
        release_manifest.release_coverage(passing, self.manifest["critical_packages"])
        below = copy.deepcopy(passing)
        below["packages"][module + "/modules/organization/management"] = {"covered_statements": 89, "total_statements": 100}
        below["overall"] = {"covered_statements": 1080, "total_statements": 1200}
        with self.assertRaisesRegex(ValueError, "below 90"):
            release_manifest.release_coverage(below, self.manifest["critical_packages"])
        exact = copy.deepcopy(passing)
        exact["packages"][module + "/modules/organization/management"] = {"covered_statements": 90, "total_statements": 100}
        exact["overall"] = {"covered_statements": 1080, "total_statements": 1200}
        release_manifest.release_coverage(exact, self.manifest["critical_packages"])
        absent = copy.deepcopy(passing)
        with self.assertRaisesRegex(ValueError, "missing critical"):
            release_manifest.release_coverage(absent, ["kernel", "persistence/noop"])
        # A high aggregate cannot mask a failing critical package.
        aggregate = copy.deepcopy(passing)
        aggregate["packages"][module + "/modules/organization/management"] = {"covered_statements": 0, "total_statements": 50}
        aggregate["overall"] = {"covered_statements": 99, "total_statements": 100}
        with self.assertRaisesRegex(ValueError, "below 90"):
            release_manifest.release_coverage(aggregate, self.manifest["critical_packages"])
        for name, mutate in {
            "float-counts": lambda c: c["packages"][module + "/kernel/fixture"].update(covered_statements=90.5),
            "negative-counts": lambda c: c["packages"][module + "/kernel/fixture"].update(total_statements=-100),
            "covered-exceeds-total": lambda c: c["packages"][module + "/kernel/fixture"].update(covered_statements=101),
            "overall-not-integer": lambda c: c["overall"].update(covered_statements="990"),
        }.items():
            with self.subTest(counts=name):
                mutated = copy.deepcopy(passing)
                mutate(mutated)
                with self.assertRaises(ValueError):
                    release_manifest.release_coverage(mutated, self.manifest["critical_packages"])

    # A95/tenant-only-build-independent
    def test_A95_tenant_only_build_independent(self):
        deps = m0.command(["go", "list", "-deps", "./internal/testkit/assemblies/tenant"]).splitlines()
        release_manifest.check_tenant_only(deps)
        with self.assertRaisesRegex(ValueError, "organization"):
            release_manifest.check_tenant_only(deps + [m0.MODULE + "/modules/organization/store"])

    def test_mapping_docs_and_release_closure(self):
        mapping = m0.load(ROOT / "tests/contract/m6.json")
        m6.validate_mapping(mapping)
        m1.check_module_docs(mapping)
        self.assertEqual(release_manifest.check_closure(self.mappings, self.groups)["mapped"], 532)
        # Repo-content release declarations all validate today; required release
        # assets are checked fail-closed by check_assets/the full gate instead.
        summary = release_manifest.check_structure(self.manifest)
        self.assertEqual(summary["requirements"], 95)
        self.assertEqual(len(release_manifest.profile_rows(ROOT)), 3)
        release_manifest.check_deliveries(self.manifest)
        release_manifest.check_versions(self.manifest, ROOT)
        release_manifest.check_event_schemas(self.manifest, ROOT)
        release_manifest.check_critical(self.manifest, ROOT)

    def test_drill_report_provenance_and_evidence(self):
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            data = drill_fixture()
            write_drills(directory, data)
            summary = release_manifest.validate_drills(data, directory, "a" * 40, "b" * 64, "windows-amd64", host_run=False)
            self.assertEqual({entry["name"] for entry in summary["entries"]}, set(release_manifest.DRILLS))
            for name, mutate in {
                "wrong-commit": lambda d: d.update(commit="c" * 40),
                "wrong-source": lambda d: d.update(source_sha256="d" * 64),
                "wrong-baseline": lambda d: d.update(baseline="v0"),
                "wrong-product-version": lambda d: d.update(product_version="0.2.0"),
                "dirty-drills-for-clean-verify": lambda d: d.update(working_tree_dirty=True, evidence_class="development-only"),
                "wrong-evidence-class": lambda d: d.update(evidence_class="development-only"),
                "inline-platform-mismatch": lambda d: d.update(platform="linux-amd64", platform_details={"GOOS": "linux", "GOARCH": "amd64"}),
                "unknown-platform": lambda d: d.update(platform="windows-arm64"),
                "platform-details-mismatch": lambda d: d["platform_details"].update(GOARCH="arm64"),
                "top-command-failed": lambda d: d["commands"][0].update(exit_code=1),
                "artifact-hash-tamper": lambda d: d["artifacts"].update({"load-baseline.json": "0" * 64}),
                "status-failed": lambda d: d["drills"]["load-baseline"].update(status="failed"),
                "command-failed": lambda d: d["drills"]["clean-rebuild"]["commands"][0].update(exit_code=1),
                "log-tampered": lambda d: d["drills"]["clean-rebuild"]["commands"][0].update(sha256="0" * 64),
                "no-commands": lambda d: d["drills"]["backup-restore"].update(commands=[]),
                "no-checks": lambda d: d["drills"]["clean-rebuild"].update(checks={}),
                "upgrade-rls-missing": lambda d: d["drills"]["forward-upgrade"]["checks"].update(force_rls=False),
                "permissive-policy": lambda d: d["drills"]["forward-upgrade"]["checks"].update(permissive_policy_left=True),
                "owner-lost": lambda d: d["drills"]["forward-upgrade"]["checks"].update(owner_preserved=False),
                "restore-unverified": lambda d: d["drills"]["backup-restore"]["checks"].update(restore_verified=False),
                "no-samples": lambda d: d["drills"]["load-baseline"]["checks"].update(samples=[]),
                "sample-count-mismatch": lambda d: d["drills"]["load-baseline"]["checks"].update(load_count=500),
                "plausible-invented-p95": lambda d: d["drills"]["load-baseline"]["checks"].update(p95_ms=22.5),
                "invented-p95": lambda d: d["drills"]["load-baseline"]["checks"].update(p95_ms=0),
                "nan-p95": lambda d: d["drills"]["load-baseline"]["checks"].update(p95_ms=float("nan")),
                "nan-sample": lambda d: d["drills"]["load-baseline"]["checks"]["samples"].__setitem__(0, float("nan")),
                "percentile-contradiction": lambda d: d["drills"]["load-baseline"]["checks"].update(p95_ms=99.0),
                "percentile-outside-samples": lambda d: d["drills"]["load-baseline"]["checks"].update(p99_ms=500.0),
                "artifact-path-escape": lambda d: d["artifacts"].update({"../escape.log": "0" * 64}),
                "missing-category": lambda d: d["drills"]["security-review"]["checks"]["categories"].pop(),
                "unresolved-high-risk": lambda d: d["drills"]["security-review"]["checks"].update(unresolved_high_risk=1),
                "no-controls": lambda d: d["drills"]["security-review"]["checks"].update(controls=[]),
                "missing-drill": lambda d: d["drills"].pop("clean-rebuild"),
            }.items():
                with self.subTest(name=name):
                    broken = copy.deepcopy(data)
                    mutate(broken)
                    with self.assertRaises(ValueError):
                        release_manifest.validate_drills(broken, directory, "a" * 40, "b" * 64, "windows-amd64", host_run=False)
            # Host-run drills keep provenance while recording their original
            # platform, and a dirty development run matches dirty drills.
            host = copy.deepcopy(data)
            release_manifest.validate_drills(host, directory, "a" * 40, "b" * 64, "linux-amd64", host_run=True)
            development = drill_fixture(platform="linux-amd64", dirty=True)
            write_drills(Path(directory) / "development", development)
            release_manifest.validate_drills(development, Path(directory) / "development", "a" * 40, "b" * 64, "linux-amd64", host_run=False, dirty=True)


if __name__ == "__main__":
    unittest.main()
