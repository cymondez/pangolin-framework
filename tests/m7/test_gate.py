"""Fail-closed unit tests for the M7 acceptance runner and additive index.

The tests run entirely against a temporary report directory with a small
explicit fixture for the milestone mappings, the named-subtest groups and the
profile plans. Every negative contract demanded by the gate is proven without
services: missing commands or logs, skipped cases, unknown source bytes,
coverage shortfalls, unknown template contracts and forged metadata all fail.
"""
import copy
import json
from contextlib import ExitStack
from pathlib import Path
from tempfile import TemporaryDirectory
import sys
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import m0
import m6_index
import m7
import m7_index
import release_manifest

COMMIT = "a" * 40
SOURCE = "b" * 64
MODULE = m0.MODULE
NA_REASON = "§13.4: tenant-only plan excludes organization-owned data"
MAPPED_REASON = "§13.4: realtime optional-module contract or applicable negative scenario"
GO_IDENTITY = MODULE + "/pkg:TestOne/one"
GO_IDENTITY_2 = MODULE + "/pkg:TestTwo/two"
REALTIME_IDENTITY = MODULE + "/modules/realtime/signalr:TestA96RT001/official-negotiate"
TAP_LOG = "\n".join([
    "TAP version 13",
    "# Subtest: tenant-only official clients and two independent live nodes",
    "ok 1 - tenant-only official clients and two independent live nodes",
    "    ok 1 - nested assertion",
    "# Subtest: organization-enabled official clients and two independent live nodes",
    "ok 2 - organization-enabled official clients and two independent live nodes",
    "# Subtest: replacement-contract official clients and two independent live nodes",
    "ok 3 - replacement-contract official clients and two independent live nodes",
    "1..3",
    "# tests 3",
    "# suites 0",
    "# pass 3",
    "# fail 0",
    "# cancelled 0",
    "# skipped 0",
    "# todo 0",
]) + "\n"
GROUPS = {"groups": [
    {"id": "A01", "milestone": "M1", "requirement": "MOD-001", "subtests": [
        {"id": "A01/one", "evidence": "artifacts/{commit}/{profile}/A01/one/"},
        {"id": "A01/two", "evidence": "artifacts/{commit}/{profile}/A01/two/"}]},
    {"id": "A45", "milestone": "M1", "requirement": "SEC-045", "subtests": [
        {"id": "A45/x", "evidence": "artifacts/{commit}/{profile}/A45/x/"}]},
    {"id": "A67", "milestone": "M5", "requirement": "CLI-001", "subtests": [
        {"id": "A67/gen", "evidence": "artifacts/{commit}/{profile}/A67/gen/"}]},
    {"id": "A72", "milestone": "M6", "requirement": "REL-005", "subtests": [
        {"id": "A72/drill", "evidence": "artifacts/{commit}/{profile}/A72/drill/"}]},
    {"id": "A96", "milestone": "M7", "requirement": "RT-001", "subtests": [
        {"id": "A96/one", "evidence": "artifacts/{commit}/{profile}/A96/one/"},
        {"id": "A96/rt", "evidence": "artifacts/{commit}/{profile}/A96/rt/"}]},
    {"id": "A104", "milestone": "M7", "requirement": "RT-009", "subtests": [
        {"id": "A104/gates", "evidence": "artifacts/{commit}/{profile}/A104/gates/"}]},
]}
NAMES = ("A01/one", "A01/two", "A45/x", "A67/gen", "A72/drill", "A96/one", "A96/rt", "A104/gates")
PROFILES = ("tenant-only", "organization-enabled", "replacement-contract")


def fixture_mappings():
    mappings = {number: {"milestone": "M" + str(number), "baseline": "v1.3"} for number in range(1, 8)}
    mappings[1]["go_tests"] = {"A01/one": GO_IDENTITY, "A01/two": GO_IDENTITY_2}
    mappings[1]["python_tests"] = {"A45/x": "tests.test_fixture.Engineering.test_x"}
    mappings[5]["template_tests"] = {"A67/gen": m7_index.TEMPLATE_CONTRACT}
    mappings[6]["drills"] = {"A72/drill": "clean-rebuild"}
    mappings[7]["unit_tests"] = {"A96/one": REALTIME_IDENTITY}
    mappings[7]["template_tests"] = {"A96/rt": m7_index.REALTIME_CONTRACT}
    mappings[7]["gate_checks"] = {"A104/gates": m7_index.GATE_CONTRACT}
    return mappings


def fixture_profiles(na_identity=None):
    manifests = {}
    for name in PROFILES:
        rows = []
        for ident in NAMES:
            applicability, reason = "required", MAPPED_REASON
            if ident == "A96/one":
                reason = "§13.4: realtime optional-module contract or applicable negative scenario"
            if name == "tenant-only" and ident == na_identity:
                applicability, reason = "not-applicable", NA_REASON
            rows.append({"id": ident, "requirement": next(g["requirement"] for g in GROUPS["groups"]
                                                          for s in g["subtests"] if s["id"] == ident),
                         "applicability": applicability, "reason": reason,
                         "evidence": "artifacts/{commit}/{profile}/" + ident + "/"})
        manifests[name] = {"profile": name, "baseline": "v1.3", "status": "planned", "subtests": rows}
    return list(zip(PROFILES, [manifests[name] for name in PROFILES]))


class IndexTestCase(unittest.TestCase):
    maxDiff = None

    def setUp(self):
        self._temporary = TemporaryDirectory()
        self.addCleanup(self._temporary.cleanup)
        self.directory = Path(self._temporary.name)
        self.hashes = {}
        self._write("01-unit.log", "\n".join([
            json.dumps({"Action": "run", "Package": MODULE + "/pkg"}),
            json.dumps({"Action": "pass", "Package": MODULE + "/pkg", "Test": "TestOne/one"}),
            json.dumps({"Action": "pass", "Package": MODULE + "/pkg", "Test": "TestTwo/two"}),
            json.dumps({"Action": "run", "Package": MODULE + "/modules/realtime/signalr"}),
            json.dumps({"Action": "pass", "Package": MODULE + "/modules/realtime/signalr", "Test": "TestA96RT001/official-negotiate"})]) + "\n")
        self._write("02-m1-regression.log",
                    "test_x (tests.test_fixture.Engineering.test_x) ... ok\nRan 1 test in 0.001s\n\nOK\n")
        self._write("03-m7-regression.log", "Ran 4 tests in 0.001s\n\nOK\n")
        self._write("04-templates.log", "Both generated Bookstore profiles passed\n")
        self._write("templates.json", json.dumps({"status": "passed", "profiles": [
            {"profile": "tenant-only", "build_test_start": True},
            {"profile": "organization-enabled", "build_test_start": True}]}))
        self._write("05-realtime-e2e.log", TAP_LOG)
        self._write(m7_index.RELEASE_E2E_FILE, json.dumps(
            {"kind": "official-client-harness", "status": "passed", "command": "realtime-e2e",
             "tests": 3, "skipped": 0, "failed": 0, "profiles": sorted(PROFILES)}))
        (self.directory / "release-drills").mkdir()
        self._write(str(Path("release-drills") / "release-drills.json"), json.dumps({
            "status": "passed", "commit": COMMIT, "source_sha256": SOURCE, "platform": "windows-amd64",
            "project": "pangolin-m6-fixture", "working_tree_dirty": False, "evidence_class": "evidence-ready",
            "drills": {name: {"status": "passed", "commands": [], "checks": {}} for name in release_manifest.DRILLS}}))
        (self.directory / "coverage.json").write_text(json.dumps(
            {"overall": {"total_statements": 1000, "covered_statements": 950},
             "packages": {MODULE + "/events/nats": {"total_statements": 100, "covered_statements": 95}}}))
        (self.directory / "release-m7.json").write_text("{}")
        self.report = {
            "commit": COMMIT, "source_sha256": SOURCE, "baseline": "v1.3", "status": "passed",
            "working_tree_dirty": False, "gate": "evidence-ready", "platform": {"GOOS": "windows", "GOARCH": "amd64"},
            "tests": [GO_IDENTITY, GO_IDENTITY_2], "integration_tests": [],
            "commands": [
                {"name": "unit", "exit_code": 0, "log": "01-unit.log", "sha256": self.hashes["01-unit.log"]},
                {"name": "m1-regression", "exit_code": 0, "log": "02-m1-regression.log", "sha256": self.hashes["02-m1-regression.log"]},
                {"name": "m7-regression", "exit_code": 0, "log": "03-m7-regression.log", "sha256": self.hashes["03-m7-regression.log"]},
                {"name": "templates", "exit_code": 0, "log": "04-templates.log", "sha256": self.hashes["04-templates.log"]},
                {"name": "realtime-e2e", "exit_code": 0, "log": "05-realtime-e2e.log", "sha256": self.hashes["05-realtime-e2e.log"]},
            ],
            "artifacts": {}}

    def _write(self, name, text):
        path = self.directory / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        self.hashes[name] = m0.digest(path)
        return path

    def _sync_hashes(self):
        for command in self.report["commands"]:
            if command["log"] in self.hashes:
                command["sha256"] = self.hashes[command["log"]]

    def _patched(self, na_identity=None):
        stack = ExitStack()
        self.addCleanup(stack.close)
        stack.enter_context(mock.patch.object(m7_index, "NAMED_SUBTESTS", len(NAMES)))
        stack.enter_context(mock.patch.object(m7_index, "REALTIME_NAMED", 3))
        gate_proof = mock.patch.object(m7_index, "_gates_proof", lambda report, directory, artifacts:
                                       {"kind": "cumulative-gates", "commit": report["commit"],
                                        "source_sha256": report["source_sha256"], "commands": [],
                                        "linux_race": False,
                                        "coverage": {"total_statements": 1, "covered_statements": 1},
                                        "artifacts": {}})
        stack.enter_context(gate_proof)
        stack.enter_context(mock.patch.object(m7_index, "_mappings", lambda root: fixture_mappings()))
        stack.enter_context(mock.patch.object(m6_index, "_subtest_groups", lambda root, max_milestone=None: GROUPS["groups"]))
        stack.enter_context(mock.patch.object(m7_index, "_profiles", lambda root, groups: fixture_profiles(na_identity)))
        return stack

    def _index(self, report=None, na_identity=None):
        self._patched(na_identity)
        return m7_index.build_index(report or self.report, self.directory, root=ROOT)

    def test_required_rows_link_exact_raw_proofs(self):
        index = self._index()
        self.assertEqual(index["totals"], {"subtests": 8, "rows": 24, "required_rows": 24, "not_applicable_rows": 0})
        unit = next(row for row in index["rows"] if row["subtest"] == "A96/one")["profiles"]["tenant-only"]["evidence"]
        self.assertEqual(unit["kind"], "go-test")
        self.assertEqual(unit["identity"], REALTIME_IDENTITY)
        gates = next(row for row in index["rows"] if row["subtest"] == "A104/gates")["profiles"]["organization-enabled"]["evidence"]
        self.assertEqual(gates["kind"], "cumulative-gates")
        self.assertIn("coverage", gates)
        evidence = next(row for row in index["rows"] if row["subtest"] == "A96/rt")["profiles"]["tenant-only"]["evidence"]
        self.assertEqual(evidence["kind"], "realtime-e2e")
        self.assertEqual(evidence["command"], "realtime-e2e")
        self.assertEqual(evidence["log"], "05-realtime-e2e.log")
        self.assertEqual(evidence["sha256"], m0.digest(self.directory / "05-realtime-e2e.log"))
        self.assertEqual(evidence["artifact_sha256"], m0.digest(self.directory / m7_index.RELEASE_E2E_FILE))
        self.assertEqual(evidence["profiles"], sorted(PROFILES))
        self.assertEqual(evidence["tests"], 3)
        self.assertTrue(evidence["shared_contract"])
        self.assertEqual(index["evidence_class"], "evidence-ready")
        written = m7_index.write_index(self.report, self.directory, root=ROOT)
        self.assertEqual(written, index)

    def test_missing_log_and_changed_log_are_rejected(self):
        (self.directory / "05-realtime-e2e.log").unlink()
        with self.assertRaises(ValueError):
            self._index()
        self._write("05-realtime-e2e.log", TAP_LOG.replace("tests 3", "tests 2"))
        with self.assertRaises(ValueError):
            self._index()

    def test_unknown_source_log_is_rejected(self):
        self.report["commands"][0]["sha256"] = "0" * 64
        with self.assertRaises(ValueError):
            self._index()

    def test_skipped_or_failed_harness_log_is_rejected(self):
        for text in (TAP_LOG.replace("ok 2 - organization-enabled", "not ok 2 - organization-enabled"),
                     TAP_LOG.replace("ok 1 - tenant-only", "ok 1 - tenant-only # SKIP unimplemented"),
                     TAP_LOG.replace("# fail 0", "# fail 1"),
                     TAP_LOG.replace("# skipped 0", "# skipped 2")):
            with self.subTest(), self._patched():
                self._write("05-realtime-e2e.log", text)
                with self.assertRaises(ValueError):
                    m7_index._realtime_proof(self.report, self.directory, {})

    def test_harness_only_reports_a_subset_of_profiles(self):
        self._patched()
        text = TAP_LOG.replace("replacement-contract official clients", "removed profile test")
        with self.assertRaises(ValueError):
            m7_index.parse_official_log(text)
        with self.assertRaises(ValueError):
            m7_index.parse_official_log(TAP_LOG.replace("ok 3 - replacement-contract", "not ok 3 - replacement-contract"))

    def test_tap_summary_must_be_complete_and_consistent(self):
        self._patched()
        expected = {"profiles": sorted(PROFILES), "tests": 3}
        self.assertEqual(m7_index.parse_official_log(TAP_LOG), expected)
        negatives = {
            "missing-cancelled": lambda t: t.replace("# cancelled 0\n", ""),
            "missing-tests": lambda t: t.replace("# tests 3\n", ""),
            "missing-pass": lambda t: t.replace("# pass 3\n", ""),
            "missing-fail": lambda t: t.replace("# fail 0\n", ""),
            "missing-skipped": lambda t: t.replace("# skipped 0\n", ""),
            "truncated-summary": lambda t: t[:t.index("# tests 3")] + "1..3\n",
            "count-mismatch": lambda t: t.replace("# tests 3", "# tests 4"),
            "pass-mismatch": lambda t: t.replace("# pass 3", "# pass 2"),
            "extra-ok": lambda t: t.replace("1..3", "1..3\nok 4 - tenant-only extra run"),
            "duplicate-profile": lambda t: t.replace(
                "ok 3 - replacement-contract official clients and two independent live nodes",
                "ok 3 - tenant-only duplicate profile run"),
            "unknown-profile": lambda t: t.replace(
                "ok 3 - replacement-contract official clients and two independent live nodes",
                "ok 3 - bootstrap-only other profile"),
        }
        for name, mutate in negatives.items():
            with self.subTest(name=name):
                with self.assertRaises(ValueError):
                    m7_index.parse_official_log(mutate(TAP_LOG))

    def test_release_e2e_artifact_is_revalidated(self):
        self._patched()
        path = self.directory / m7_index.RELEASE_E2E_FILE
        original = json.loads(self.hashes and json.dumps(
            {"kind": "official-client-harness", "status": "passed", "command": "realtime-e2e",
             "tests": 3, "skipped": 0, "failed": 0, "profiles": sorted(PROFILES)}))
        mutations = (lambda d: d.update(status="forged"),
                     lambda d: d.update(profiles=["tenant-only"]),
                     lambda d: d.update(skipped=1),
                     lambda d: d.update(tests=1))
        for mutation in mutations:
            data = copy.deepcopy(original)
            mutation(data)
            path.write_text(json.dumps(data), encoding="utf-8")
            with self.assertRaises(ValueError):
                m7_index.read_release_e2e(self.directory)
        path.write_text(json.dumps(original, indent=2), encoding="utf-8")

    def test_realtime_e2e_json_tamper_fails_index(self):
        self._patched()
        path = self.directory / m7_index.RELEASE_E2E_FILE
        path.write_text(path.read_text(encoding="utf-8").replace("replacement-contract", "replacement"), encoding="utf-8")
        with self.assertRaises(ValueError):
            self._index()

    def test_not_applicable_row_preserves_reason_without_pass_claim(self):
        index = self._index(na_identity="A96/one")
        self.assertEqual(index["totals"]["required_rows"], 23)
        row = next(row for row in index["rows"] if row["subtest"] == "A96/one")["profiles"]["tenant-only"]
        self.assertEqual(row["applicability"], "not-applicable")
        self.assertEqual(row["reason"], NA_REASON)
        self.assertIsNone(row["evidence"])
        self.assertNotIn("status", row)
        self.assertNotIn("passed", row)
        shared = next(row for row in index["rows"] if row["subtest"] == "A96/one")["profiles"]["organization-enabled"]["evidence"]
        self.assertEqual(shared["identity"], REALTIME_IDENTITY)

    def test_unknown_template_contract_is_rejected(self):
        self._patched()
        mappings = fixture_mappings()
        mappings[7]["template_tests"] = {"A96/one": "single-profile-build"}
        with mock.patch.object(m7_index, "_mappings", lambda root: mappings):
            with self.assertRaises(ValueError):
                m7_index.build_index(self.report, self.directory, root=ROOT)

    def test_incomplete_mapping_closure_is_rejected(self):
        self._patched()
        mappings = fixture_mappings()
        del mappings[7]["unit_tests"]["A96/one"]
        with mock.patch.object(m7_index, "_mappings", lambda root: mappings):
            with self.assertRaises(ValueError):
                m7_index.build_index(self.report, self.directory, root=ROOT)

    def test_log_path_escape_is_rejected(self):
        self._patched()
        self.report["commands"][0]["log"] = "../escape.log"
        with self.assertRaises(ValueError):
            self._index()

    def test_development_reports_are_never_labelled_verified(self):
        self.assertEqual(self._index()["evidence_class"], "evidence-ready")
        self.report["working_tree_dirty"] = True
        self.assertEqual(self._index()["evidence_class"], "development-only")
        self.report["working_tree_dirty"] = False
        self.report["gate"] = "development-only"
        self.assertEqual(self._index()["evidence_class"], "development-only")


class CoverageThresholdTestCase(unittest.TestCase):
    def _coverage(self, overall, nats):
        return {"overall": overall, "packages": {MODULE + "/events/nats": nats,
                                                 MODULE + "/modules/realtime/signalr": {"total_statements": 100, "covered_statements": 99}}}

    def test_ninety_percent_and_each_critical_package_is_enforced(self):
        coverage = self._coverage({"total_statements": 1000, "covered_statements": 900},
                                  {"total_statements": 100, "covered_statements": 90})
        result = release_manifest.release_coverage(coverage, ["events/nats", "modules/realtime"])
        self.assertEqual(result["overall"]["total_statements"], 1000)
        self.assertEqual([entry["package"] for entry in result["critical"]], [MODULE + "/events/nats", MODULE + "/modules/realtime/signalr"])

    def test_below_ninety_percent_fails_closed(self):
        base = self._coverage({"total_statements": 1000, "covered_statements": 899},
                              {"total_statements": 100, "covered_statements": 90})
        with self.assertRaises(ValueError):
            release_manifest.release_coverage(base, ["events/nats"])
        low_critical = self._coverage({"total_statements": 1000, "covered_statements": 900},
                                      {"total_statements": 100, "covered_statements": 89})
        with self.assertRaises(ValueError):
            release_manifest.release_coverage(low_critical, ["events/nats", "modules/realtime"])
        with self.assertRaises(ValueError):
            release_manifest.release_coverage(self._coverage({"total_statements": 1000, "covered_statements": 1001},
                                                             {"total_statements": 100, "covered_statements": 90}), ["events/nats"])
        foreign = self._coverage({"total_statements": 1000, "covered_statements": 900},
                                 {"total_statements": 100, "covered_statements": 90})
        foreign["packages"]["github.com/foreign/pkg"] = {"total_statements": 10, "covered_statements": 9}
        with self.assertRaises(ValueError):
            release_manifest.release_coverage(foreign, ["events/nats"])


class MappingAndManifestTestCase(unittest.TestCase):
    def _synthetic_complete_mapping(self):
        """61-subtest mapping shape the test slice must produce when complete."""
        groups = m0.load(ROOT / "tests/contract/subtests.json")["groups"]
        mapping = {"milestone": "M7", "baseline": "v1.3", "pending": []}
        for field in m7.FIELDS:
            mapping[field] = {}
        for group in groups:
            if group["milestone"] != "M7":
                continue
            mapping["unit_tests"] = mapping["unit_tests"] if isinstance(mapping["unit_tests"], dict) else {}
            for subtest in group["subtests"]:
                if subtest["id"] == "A104/cumulative-gates":
                    mapping["gate_checks"][subtest["id"]] = m7_index.GATE_CONTRACT
                    continue
                identity = subtest.get("test")
                if identity:
                    mapping["unit_tests"][subtest["id"]] = identity
                else:
                    mapping["template_tests"][subtest["id"]] = m7_index.REALTIME_CONTRACT
        return mapping

    def test_real_m7_manifest_binds_104_requirements(self):
        summary = m7.validate_m7_manifest(m0.load(ROOT / "deploy/release/m7.json"))
        self.assertEqual(summary["requirements"], 104)
        self.assertEqual(summary["subtests"], 593)
        self.assertIn("events/nats", summary["critical_packages"])
        self.assertIn("modules/realtime", summary["critical_packages"])

    def test_a_complete_mapping_without_pending_passes(self):
        mapping = self._synthetic_complete_mapping()
        m7.validate_mapping(mapping)
        self.assertEqual(len(set(mapping["unit_tests"]) | set(mapping["template_tests"]) | set(mapping["gate_checks"])), 61)

    def test_pending_must_be_empty(self):
        mapping = self._synthetic_complete_mapping()
        mapping["pending"] = [{"id": next(iter(mapping["unit_tests"]))}]
        with self.assertRaises(ValueError):
            m7.validate_mapping(mapping)

    def test_mapping_negatives(self):
        complete = self._synthetic_complete_mapping()
        negatives = {
            "pending-nonempty": lambda m: m.update(pending=[{"id": next(iter(m["unit_tests"]))}]),
            "incomplete": lambda m: m["unit_tests"].pop(next(iter(m["unit_tests"]))),
            "unknown-subtest": lambda m: m["unit_tests"].update({"A96/not-a-subtest": MODULE + "/modules/realtime:TestX/x"}),
            "empty-identity": lambda m: m["unit_tests"].update({next(iter(m["unit_tests"])): ""}),
            "go-identity-shape": lambda m: m["unit_tests"].update({next(iter(m["unit_tests"])): "some:Plain"}),
            "unsupported-template": lambda m: m["template_tests"].update(
                {next(iter(m["template_tests"])): "compile-only"}),
            "gate-contract": lambda m: m["gate_checks"].update({"A104/cumulative-gates": "self-attested"}),
            "wrong-milestone": lambda m: m.update(milestone="M6"),
            "wrong-baseline": lambda m: m.update(baseline="v0"),
        }
        for name, mutate in negatives.items():
            with self.subTest(name=name):
                mapping = copy.deepcopy(complete)
                mutate(mapping)
                with self.assertRaises(ValueError):
                    m7.validate_mapping(mapping)

    def test_tenant_only_excludes_only_org_owned_semantics(self):
        summary = m7.validate_m7_manifest(m0.load(ROOT / "deploy/release/m7.json"))
        rows = {row["profile"]: row for row in summary["profiles"]}
        self.assertEqual(rows["tenant-only"]["required"], 512)
        self.assertEqual(rows["tenant-only"]["not_applicable"], 81)
        self.assertEqual((rows["organization-enabled"]["required"], rows["organization-enabled"]["not_applicable"]), (593, 0))
        self.assertEqual((rows["replacement-contract"]["required"], rows["replacement-contract"]["not_applicable"]), (593, 0))
        profile = m0.load(ROOT / "tests/contract/profiles/tenant-only.json")
        plan = {entry["id"]: entry for entry in profile["subtests"]}
        org_owned = {"A98/enterprise-child-negative", "A98/multi-membership", "A99/disabled-scope"}
        for ident in org_owned:
            with self.subTest(subtest=ident):
                self.assertEqual(plan[ident]["applicability"], "not-applicable")
                self.assertEqual(plan[ident]["status"], "not-applicable")
                self.assertTrue(plan[ident]["reason"].strip().startswith("§13.4:"))
        # General tenant checks and the tenant-side negatives of the same
        # requirement groups stay required — nothing is omitted.
        for ident in ("A98/tenant-collision", "A98/unsubscribe", "A98/user-devices", "A98/scope-collision",
                      "A99/invalid-credential", "A99/membership-revocation", "A99/permission-deny", "A96/token-binding"):
            with self.subTest(subtest=ident):
                self.assertEqual(plan[ident]["applicability"], "required")
        # Every M7 named subtest stays mapped in the acceptance contract; with
        # the worker's pending entries still open, the closure equality in
        # validate_mapping locks this once the mapping finalizes.
        mapping = m0.load(ROOT / "tests/contract/m7.json")
        if not mapping.get("pending"):
            covered = set()
            for field in m7.FIELDS:
                covered |= set(mapping.get(field) or {})
            self.assertTrue(org_owned <= covered)

    def test_m7_manifest_negatives(self):
        manifest = m0.load(ROOT / "deploy/release/m7.json")
        negatives = {
            "unknown-key": lambda m: m.update(extra="x"),
            "missing-key": lambda m: m.pop("version_bindings"),
            "requirements-drift": lambda m: m.update(requirements=95),
            "subtests-drift": lambda m: m.update(subtests=532),
            "version-scope-drift": lambda m: m.update(version_scope="v0.2.0"),
            "critical-drift": lambda m: m.update(critical_packages=["events/nats"]),
            "drill-drift": lambda m: m.update(drills=["clean-rebuild"]),
            "asset-missing": lambda m: m.update(assets=["deploy/release/m7.json"]),
            "profile-source-drift": lambda m: m["profiles"][0].update(applicability_source="tests/contract/profiles/replacement.json"),
        }
        for name, mutate in negatives.items():
            with self.subTest(name=name):
                edited = copy.deepcopy(manifest)
                mutate(edited)
                with self.assertRaises(ValueError):
                    m7.validate_m7_manifest(edited)


class GateInventoryTestCase(unittest.TestCase):
    def test_full_inventory_tracks_every_present_hand_written_package(self):
        full = m0.go_packages(tags=True)
        names = [name for name in m0.command(["git", "-c", "core.quotepath=false", "ls-files", "--cached", "--others", "--exclude-standard", "--", "*.go"]).splitlines() if name.strip()]
        names = [name for name in names if (ROOT / name).is_file()
                 and ".." not in Path(name).parts and not name.startswith(("..", "/", "\\"))]
        # Nested modules (go.mod inside the tree) are separate module units and
        # are excluded for either list, exactly like historical ./....
        def nested(name):
            parts = [part for part in name.split("/") if part]
            return any((ROOT / Path(*parts[:index + 1]) / "go.mod").is_file() for index in range(len(parts) - 1))
        expected = sorted({"./" + name.rsplit("/", 1)[0] if "/" in name else "./" for name in names if not nested(name)})
        self.assertEqual(expected, full)
        self.assertIn("./", full)
        self.assertFalse(any(x in {"./examples/basics", "./examples/moduleapp"} or x.startswith(("./examples/basics/", "./examples/moduleapp/")) for x in full))
        for expected_dir in ("./modules/realtime", "./modules/realtime/signalr", "./modules/realtime/jetstream",
                             "./events/nats", "./tests/m7", "./tests/m6", "./examples/realtime", "./cmd/pangolin"):
            self.assertIn(expected_dir, full)

    def test_default_inventory_matches_historical_pattern_selection(self):
        default, full = m0.go_packages(), m0.go_packages(tags=True)
        # Ignored generated/bug-reproducer directories stay excluded.
        self.assertFalse(any("/artifacts" in d or d == "./artifacts" for d in full))
        # Integration-tagged suites and other constraint-excluded packages run
        # only under -tags=integration, exactly as ./... skipped them.
        for tagged in ("./tests/m1", "./tests/m7", "./tests/database", "./tests/designcorrections"):
            self.assertNotIn(tagged, default)
            self.assertIn(tagged, full)
        # Every tracked production/test package of the main module is present
        # under default constraints, and the build subset drops test-only dirs.
        self.assertIn("./tests/rootmodules", default)
        self.assertNotIn("./tests/rootmodules", m0.go_build_packages())
        self.assertLessEqual(set(m0.go_build_packages()), set(default))

    def test_ignored_gate_tempdir_module_keeps_legacy_go_list_semantics(self):
        # The gate runs go test under m0.go_environment with GOTMPDIR inside
        # the repository; go test then redirects t.TempDir beneath the ignored
        # .cache tree. Such a fixture root resolves git ancestors but owns no
        # git scope, so it must use the historical `go list ./...` inventory.
        import tempfile
        (ROOT / ".cache" / "go-tmp").mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache" / "go-tmp") as temporary:
            Path(temporary, "kernel", "check").mkdir(parents=True, exist_ok=True)
            Path(temporary, "go.mod").write_text("module github.com/cymondez/pangolin-framework\n\ngo 1.27.0\n", encoding="utf-8")
            Path(temporary, "kernel", "check", "f.go").write_text("package check\nfunc F() int { return 1 }\n", encoding="utf-8")
            self.assertEqual(m0._go_git_scope(temporary), False)
            self.assertEqual(m0.go_packages(root=temporary), ["./kernel/check"])


class GateChecksProofTestCase(unittest.TestCase):
    """The cumulative-gate proof must bind real executed evidence fail-closed."""

    maxDiff = None

    raw_artifacts = {
        "merged.out": "mode: atomic\n",
        "go-list.json": "{}\n",
        "exclusions.json": "{}\n",
        "dependencies.json": "{}\n",
        "templates.json": json.dumps({"status": "passed", "profiles": []}) + "\n",
        "client-dependencies.json": json.dumps({"packages": {}}) + "\n",
    }

    def _report(self, directory, **overrides):
        logs = {"02-integration.log": [{"Action": "run", "Package": "p"},
                                       {"Action": "pass", "Package": "p", "Test": "T"}],
                "07-coverage.log": [{"Action": "pass", "Package": "p", "Test": "T"}],
                "08-race.log": [{"Action": "pass", "Package": "p", "Test": "T"}],
                "21-release-manifest.log": ["release evidence"]}
        written = {}
        for name, payload in logs.items():
            text = "\n".join(item if isinstance(item, str) else json.dumps(item) for item in payload) + "\n"
            path = directory / name
            path.write_text(text, encoding="utf-8")
            written[name] = m0.digest(path)
        report = {
            "commit": COMMIT, "source_sha256": SOURCE, "baseline": "v1.3", "status": "passed",
            "working_tree_dirty": False, "gate": "evidence-ready",
            "platform": {"GOOS": "windows", "GOARCH": "amd64"},
            "release_drills": {"host_run": False},
            "commands": [
                {"name": name, "exit_code": 0} for name in
                ["inventory", "build", "vet", "go-module-verify", "unit", "integration", "e2e", "coverage",
                 "coverage-html", "generation", "generation-version", "m0-regression", "m1-regression",
                 "m2-regression", "m3-regression", "m4-regression", "m5-regression", "m6-regression",
                 "m7-regression", "templates", "template-source", "cli-build", "cli-coverage",
                 "release-manifest", "node-version", "npm-version", "realtime-dbtool", "realtime-db-create",
                 "realtime-demo", "realtime-provision", "realtime-broker-cleanup", "realtime-client-deps",
                 "realtime-client-licenses", "realtime-e2e",
                 "realtime-coverage", "realtime-db-drop", "release-drills", "templates-compose"]
            ] + [{"name": "e2e-step", "exit_code": 0} for _ in range(4)],
            "artifacts": {},
        }
        for name, body in self.raw_artifacts.items():
            (directory / name).write_text(body, encoding="utf-8")
        for command in report["commands"]:
            log = {"integration": "02-integration.log", "coverage": "07-coverage.log",
                   "race": "08-race.log", "release-manifest": "21-release-manifest.log"}.get(command["name"])
            if log:
                command["log"], command["sha256"] = log, written[log]
        report.update(overrides)
        (directory / "coverage.json").write_text(json.dumps(
            {"overall": {"total_statements": 1000, "covered_statements": 950},
             "packages": {}}), encoding="utf-8")
        report["artifacts"] = {
            "coverage.json": m0.digest(directory / "coverage.json"),
            "merged.out": m0.digest(directory / "merged.out"),
            "go-list.json": m0.digest(directory / "go-list.json"),
            "exclusions.json": m0.digest(directory / "exclusions.json"),
            "dependencies.json": m0.digest(directory / "dependencies.json"),
            "templates.json": m0.digest(directory / "templates.json"),
            "client-dependencies.json": m0.digest(directory / "client-dependencies.json")}
        return report

    def test_complete_windows_proof_passes_without_circular_hash(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report = self._report(directory)
            proof = m7_index._gates_proof(report, directory, report["artifacts"])
            self.assertEqual(proof["kind"], "cumulative-gates")
            self.assertEqual(proof["commit"], COMMIT)
            self.assertIn("coverage.json", proof["artifacts"])
            self.assertNotIn("report", proof)

    def test_missing_failed_duplicated_commands_fail(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report = self._report(directory)
            trimmed = self._report(directory)
            trimmed["commands"] = [c for c in trimmed["commands"] if c["name"] != "vet"]
            with self.assertRaises(ValueError):
                m7_index._gates_proof(trimmed, directory, trimmed["artifacts"])
            broken = self._report(directory)
            broken["commands"][0]["exit_code"] = 1
            with self.assertRaises(ValueError):
                m7_index._gates_proof(broken, directory, broken["artifacts"])
            duplicated = self._report(directory)
            duplicated["commands"].append({"name": "vet", "exit_code": 0})
            with self.assertRaises(ValueError):
                m7_index._gates_proof(duplicated, directory, duplicated["artifacts"])
            thin = self._report(directory)
            thin["commands"] = [c for c in thin["commands"] if c["name"] != "e2e-step"]
            with self.assertRaises(ValueError):
                m7_index._gates_proof(thin, directory, thin["artifacts"])

    def test_missing_binding_artifacts_and_low_coverage_fail(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report = self._report(directory)
            # The coverage union is read back by the proof, so a raw run that
            # never produced it fails closed (in the real runner the hook
            # cannot be reached without the coverage gate first).
            (directory / "coverage.json").unlink()
            try:
                with self.assertRaises((ValueError, OSError)):
                    m7_index._gates_proof(report, directory, report["artifacts"])
            finally:
                (directory / "coverage.json").write_text(
                    json.dumps({"overall": {"total_statements": 1000, "covered_statements": 950}, "packages": {}}),
                    encoding="utf-8")
            untouched = self._report(directory)
            # A finalized bound digest that contradicts the raw artifact is
            # rejected outright (the hook path records from disk instead).
            tampered_map = self._report(directory)
            tampered_map["artifacts"]["coverage.json"] = "f" * 64
            with self.assertRaises(ValueError):
                m7_index._gates_proof(tampered_map, directory, tampered_map["artifacts"])
            tampered = self._report(directory)
            (directory / "coverage.json").write_text(
                json.dumps({"overall": {"total_statements": 1000, "covered_statements": 1001}, "packages": {}}),
                encoding="utf-8")
            with self.assertRaises(ValueError):
                m7_index._gates_proof(tampered, directory, tampered["artifacts"])
            (directory / "coverage.json").write_text(
                json.dumps({"overall": {"total_statements": 1000, "covered_statements": 899}, "packages": {}}),
                encoding="utf-8")
            with self.assertRaises(ValueError):
                m7_index._gates_proof(tampered, directory, tampered["artifacts"])

    def test_linux_requires_the_race_command(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report = self._report(directory, platform={"GOOS": "linux", "GOARCH": "amd64"})
            report["commands"].append({"name": "race", "exit_code": 0, "log": "08-race.log",
                                       "sha256": m0.digest(directory / "08-race.log")})
            with self.assertRaises(ValueError):
                m7_index._gates_proof({**report, "commands": [c for c in report["commands"] if c.get("name") != "race"]},
                                      directory, report["artifacts"])
            proof = m7_index._gates_proof(report, directory, report["artifacts"])
            self.assertTrue(proof["linux_race"])

    def test_classification_rejects_unknown_gate_contract(self):
        with self.assertRaises(ValueError):
            m7_index._classify("gate_checks", "self-attested")


class ClientDependenciesProofTestCase(unittest.TestCase):
    """The raw client-dependencies artifact is re-derived against the reviewed manifest."""

    def test_missing_artifact_fails_closed(self):
        with TemporaryDirectory() as temporary:
            with self.assertRaises((OSError, ValueError)):
                m7.validate_client_dependencies(Path(temporary))

    def test_tampered_actual_inventory_is_rejected(self):
        import m7_client_dependencies
        with TemporaryDirectory() as temporary:
            actual = m7_client_dependencies.inventory()
            manifest = {"package_json_sha256": actual["package_json_sha256"],
                        "lockfile_sha256": actual["lockfile_sha256"],
                        "packages": actual["packages"], "allowed_licenses": ["MIT", "BSD-2-Clause", "BSD-3-Clause", "Unlicense"]}
            m7_client_dependencies.validate(manifest, actual)
            forged = json.loads(json.dumps(actual))
            forged["package_json_sha256"] = "0" * 64
            with self.assertRaises(ValueError):
                m7_client_dependencies.validate(manifest, forged)
            dropped = json.loads(json.dumps(actual))
            dropped["packages"].pop(next(iter(dropped["packages"])))
            with self.assertRaises(ValueError):
                m7_client_dependencies.validate(manifest, dropped)
            unknown = json.loads(json.dumps(actual))
            name = next(iter(unknown["packages"]))
            unknown["packages"][name]["license"] = "Proprietary"
            with self.assertRaises(ValueError):
                m7_client_dependencies.validate(manifest, unknown)

    def test_committed_manifest_and_required_command_stay_wired(self):
        self.assertEqual(m7.validate_mapping(m0.load(ROOT / "tests/contract/m7.json"))["gate_checks"],
                         {"A104/cumulative-gates": m7_index.GATE_CONTRACT})
        manifest = m0.load(ROOT / "deploy/release/m7.json")
        self.assertIn("deploy/release/m7-client-dependencies.json", manifest["assets"])


class RealtimePreparedGateTestCase(unittest.TestCase):
    """The M7 cross-platform gate must not require the per-run realtime
    fixture identity to match: the random database name and the intentionally
    Linux-only child race differ between correct Windows and Linux reports,
    so they are validated strictly per platform and only the platform-
    independent invariants (provisioned, covered_child_binary) are compared."""

    def _report(self, goos, database, race, binary):
        return {"platform": {"GOOS": goos, "GOARCH": "amd64"},
                "prepare": {"realtime": {"database": database, "database_tool": "realtime-db.exe",
                                         "binary": binary, "provisioned": True, "race": race,
                                         "coverage_dir": "realtime-cov", "covered_child_binary": True}},
                "artifacts": {binary: "0" * 64}}

    def _evidence(self, database, child_race):
        return {"status": "passed", "baseline": "v1.3", "product_version": "0.1.0",
                "version_scope": "0.1.0-additive", "versions": {"runtime": "0.1.0"},
                "requirements": 104, "named_subtests": 593, "profiles": 3, "closure": {},
                "regressions": {}, "critical_packages": [], "template_profiles": ["tenant-only"],
                "realtime_official_client": {"status": "passed", "profiles": 3, "tests": 3},
                "realtime_prepared": {"database": database, "provisioned": True,
                                      "covered_child_binary": True, "child_race": child_race},
                "client_dependencies": {"status": "locked"},
                "manifest_sha256": "0" * 64, "release_manifest_sha256": "1" * 64, "gates": []}

    def test_differing_database_and_child_race_are_valid_per_platform(self):
        windows = self._report("windows", "m5_template_m7rt_" + "1a2b3c4d5e6f", False, "realtime-demo.exe")
        linux = self._report("linux", "m5_template_m7rt_" + "7f8e9d0c1b2a", True, "realtime-demo")
        self.assertNotEqual(windows["prepare"]["realtime"]["database"], linux["prepare"]["realtime"]["database"])
        self.assertNotEqual(windows["prepare"]["realtime"]["race"], linux["prepare"]["realtime"]["race"])
        m7.validate_realtime_prepared(windows, windows["artifacts"])
        m7.validate_realtime_prepared(linux, linux["artifacts"])

    def test_differing_database_and_child_race_pass_the_cross_platform_gate(self):
        m7.compare_release_declarations(self._evidence("m5_template_m7rt_" + "1a2b3c4d5e6f", False),
                                        self._evidence("m5_template_m7rt_" + "7f8e9d0c1b2a", True))

    def test_wrongly_configured_child_race_fails_per_platform(self):
        raced_windows = self._report("windows", "m5_template_m7rt_" + "1a2b3c4d5e6f", True, "realtime-demo.exe")
        with self.assertRaises(ValueError):
            m7.validate_realtime_prepared(raced_windows, raced_windows["artifacts"])
        unraced_linux = self._report("linux", "m5_template_m7rt_" + "7f8e9d0c1b2a", False, "realtime-demo")
        with self.assertRaises(ValueError):
            m7.validate_realtime_prepared(unraced_linux, unraced_linux["artifacts"])
        absent_race = self._report("linux", "m5_template_m7rt_" + "7f8e9d0c1b2a", True, "realtime-demo")
        del absent_race["prepare"]["realtime"]["race"]
        with self.assertRaises(ValueError):
            m7.validate_realtime_prepared(absent_race, absent_race["artifacts"])

    def test_database_identity_format_fails_closed(self):
        for database in ("m5_template_m7rt_" + "1a2b3c4d5e6", "m5_template_m7rt_" + "1a2b3c4d5e6ff",
                         "m5_template_m7rt_" + "g1a2b3c4d5e6", "m5_template_m7rtX" + "1a2b3c4d5e6f", 17):
            forged = self._report("linux", database, True, "realtime-demo")
            with self.assertRaises(ValueError):
                m7.validate_realtime_prepared(forged, forged["artifacts"])

    def test_missing_prepared_invariants_fail_per_platform(self):
        for field in ("provisioned", "covered_child_binary"):
            forged = self._report("linux", "m5_template_m7rt_" + "1a2b3c4d5e6f", True, "realtime-demo")
            forged["prepare"]["realtime"][field] = False
            with self.assertRaises(ValueError):
                m7.validate_realtime_prepared(forged, forged["artifacts"])
        unrecorded = self._report("linux", "m5_template_m7rt_" + "1a2b3c4d5e6f", True, "realtime-demo")
        unrecorded["artifacts"] = {}
        with self.assertRaises(ValueError):
            m7.validate_realtime_prepared(unrecorded, unrecorded["artifacts"])
        with self.assertRaises(ValueError):
            m7.validate_realtime_prepared({"platform": {"GOOS": "linux", "GOARCH": "amd64"}, "artifacts": {}}, {})

    def test_realtime_invariant_and_declaration_divergence_still_gates(self):
        windows = self._evidence("m5_template_m7rt_" + "1a2b3c4d5e6f", False)
        linux = self._evidence("m5_template_m7rt_" + "7f8e9d0c1b2a", True)
        for field in ("provisioned", "covered_child_binary"):
            forged = copy.deepcopy(linux)
            forged["realtime_prepared"][field] = False
            with self.assertRaises(ValueError):
                m7.compare_release_declarations(windows, forged)
        declared = copy.deepcopy(linux)
        declared["requirements"] = 103
        with self.assertRaises(ValueError):
            m7.compare_release_declarations(windows, declared)


class ReleaseEvidenceImmutabilityTestCase(unittest.TestCase):
    """The stored release evidence must be validated strictly read-only.

    The evidence artifact is emitted once by the verify hook with canonical
    LF bytes; gate-time validation re-derives the evidence purely from the
    raw artifacts and compares the stored content plus the recorded artifact
    digest, never writing, rewriting or re-emitting the stored file. The
    historical bug rewrote release-m7.json inside validation with platform
    newlines, silently breaking its recorded artifact hash across platforms.
    """

    maxDiff = None

    # Milestones without python mappings still need a real, non-empty
    # unittest run: one genuine filler case satisfies the validator.
    _FILLER_CASE = "test_gate.FixtureSuite.test_noop ... ok\n"

    def _snapshot(self, directory):
        return {str(path.relative_to(directory)): (path.read_bytes(), path.stat().st_mtime_ns)
                for path in sorted(Path(directory).rglob("*")) if path.is_file()}

    def _fixture(self, directory):
        """A minimal cumulative report directory bound to the committed contract.

        Every raw artifact the pure derivation reads is generated from the
        real acceptance contract (mappings, release manifest, drills, the
        installed client tree), so the store-then-validate round trip below
        exercises exactly the derivation the live gate performs."""
        mapping = m0.load(ROOT / "tests/contract/m7.json")
        regression_integration = mapping.get("regression_integration_tests") or {}
        regression_integration = set(regression_integration.values()) if isinstance(regression_integration, dict) else set(regression_integration)
        report = {
            "commit": COMMIT, "source_sha256": SOURCE, "baseline": "v1.3", "status": "passed",
            "working_tree_dirty": False, "gate": "evidence-ready", "requested_race": False,
            "platform": {"GOOS": "windows", "GOARCH": "amd64"},
            "tests": sorted(set(mapping["unit_tests"].values())),
            "integration_tests": sorted(set(mapping["integration_tests"].values()) | regression_integration),
            "prepare": {"realtime": {"database": "m5_template_m7rt_" + "1a2b3c4d5e6f",
                                     "database_tool": "realtime-db.exe", "binary": "realtime-demo.exe",
                                     "provisioned": True, "race": False, "coverage_dir": "realtime-cov",
                                     "covered_child_binary": True}},
            "release_drills": {"host_run": False, "project": "pangolin-m7-fixture"},
            "commands": [], "artifacts": {}}
        for number in range(0, 8):
            name = f"m{number}-regression"
            required = () if number in (0, 7) else sorted(set(
                m0.load(ROOT / f"tests/contract/m{number}.json").get("python_tests", {}).values()))
            body = "".join(identity + " ... ok\n" for identity in required) + \
                (self._FILLER_CASE if not required else "") + \
                f"Ran {max(len(required), 1)} tests in 0.001s\n\nOK\n"
            log = directory / f"{number:02d}-{name}.log"
            log.write_bytes(body.encode("utf-8"))
            report["commands"].append({"name": name, "exit_code": 0, "log": log.name,
                                       "sha256": m0.digest(log)})
        critical = sorted(set(m0.load(ROOT / "deploy/release/m7.json")["critical_packages"]))
        packages = {MODULE + "/" + prefix: {"total_statements": 100, "covered_statements": 95} for prefix in critical}
        (directory / "coverage.json").write_bytes(json.dumps(
            {"overall": {"total_statements": 1000, "covered_statements": 950}, "packages": packages},
            indent=2, sort_keys=True).encode("utf-8") + b"\n")
        (directory / "templates.json").write_bytes(json.dumps(
            {"status": "passed", "profiles": [{"profile": name} for name in m7_index.OFFICIAL_PROFILES]}).encode("utf-8") + b"\n")
        (directory / m7_index.RELEASE_E2E_FILE).write_bytes(json.dumps(
            {"kind": "official-client-harness", "status": "passed", "command": "realtime-e2e",
             "tests": 3, "skipped": 0, "failed": 0, "profiles": sorted(m7_index.OFFICIAL_PROFILES)}).encode("utf-8") + b"\n")
        import m7_client_dependencies
        inventory = m7_client_dependencies.inventory()
        (directory / "client-dependencies.json").write_bytes(
            (json.dumps(inventory, indent=2, sort_keys=True) + "\n").encode("utf-8"))
        report["artifacts"]["client-dependencies.json"] = m0.digest(directory / "client-dependencies.json")
        drills = directory / "release-drills"
        drills.mkdir()
        samples = [150 + rank for rank in range(50)]
        checks = {
            "clean-rebuild": {"clean_source": True, "clean_database": True, "clean_build": True, "tests_passed": True},
            "forward-upgrade": {"seeded_tenant_rows": 3, "owner_preserved": True, "force_rls": True,
                                "permissive_policy_left": False},
            "backup-restore": {"restore_verified": True, "row_counts_match": True,
                               "checksums_match": True, "independent_connection": True},
            "load-baseline": {"hardware": "fixture cpu", "go_version": "pinned toolchain",
                              "database": "pinned postgres", "broker": "pinned nats",
                              "methodology": "fixture harness", "pool_size": 2, "load_count": len(samples),
                              "concurrency": 4, "p95_ms": sorted(samples)[(len(samples) * 95) // 100],
                              "p99_ms": sorted(samples)[(len(samples) * 99) // 100], "samples": samples},
            "security-review": {"categories": list(release_manifest.THREAT_CATEGORIES), "unresolved_high_risk": 0,
                                "controls": [{"category": category, "attack_surface": "fixture surface",
                                              "abuse_path": "fixture abuse path", "control": "fixture control",
                                              "test": "fixture test", "residual_risk": "fixture residual"}
                                             for category in release_manifest.THREAT_CATEGORIES]}}
        entries = {}
        for name in release_manifest.DRILLS:
            log = name.replace("-", "_") + ".log"
            (drills / log).write_bytes(f"{name}: fixture drill commands executed\n".encode("utf-8"))
            entries[name] = {"status": "passed",
                             "commands": [{"argv": ["fixture", "--drill", name], "exit_code": 0,
                                           "log": log, "sha256": m0.digest(drills / log)}],
                             "checks": checks[name]}
        artifact = drills / "drill-summary.json"
        artifact.write_bytes(b"fixture drill artifact\n")
        (drills / "release-drills.json").write_bytes(json.dumps(
            {"status": "passed", "baseline": "v1.3", "product_version": "0.1.0",
             "commit": COMMIT, "source_sha256": SOURCE, "working_tree_dirty": False,
             "evidence_class": "evidence-ready", "platform": {"GOOS": "windows", "GOARCH": "amd64"},
             "project": "pangolin-m7-fixture", "drills": entries,
             "artifacts": {"drill-summary.json": m0.digest(artifact)}}).encode("utf-8") + b"\n")
        evidence = m7.build_release_m7(report, directory)
        path = m7.write_release_evidence(evidence, directory)
        report["artifacts"][m7_index.RELEASE_EVIDENCE_FILE] = m0.digest(path)
        return report, evidence, path

    def test_write_is_canonical_lf_and_only_once(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            evidence = {"b": 2, "a": "x"}
            path = m7.write_release_evidence(evidence, directory)
            raw = path.read_bytes()
            self.assertEqual(raw, (json.dumps(evidence, indent=2, sort_keys=True) + "\n").encode("utf-8"))
            self.assertNotIn(b"\r\n", raw)
            with self.assertRaises(ValueError):
                m7.write_release_evidence({"a": "tampered"}, directory)
            self.assertEqual(raw, path.read_bytes())

    def test_stored_lf_artifact_validates_without_writes(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report, evidence, path = self._fixture(directory)
            before = self._snapshot(directory)
            self.assertEqual(m7.validate_stored_release_m7(report, directory), evidence)
            self.assertEqual(before, self._snapshot(directory),
                             "validation rewrote or retouched the stored evidence bytes/mtimes")

    def test_stored_crlf_artifact_validates_without_modification(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report, evidence, path = self._fixture(directory)
            # A historical CRLF artifact (legacy platform-newline emission) is
            # accepted read-only: content equality is JSON equality while the
            # recorded artifact digest keeps the exact stored bytes.
            crlf = (json.dumps(evidence, indent=2, sort_keys=True) + "\n").encode("utf-8").replace(b"\n", b"\r\n")
            path.write_bytes(crlf)
            report["artifacts"][m7_index.RELEASE_EVIDENCE_FILE] = m0.digest(path)
            before = self._snapshot(directory)
            self.assertEqual(m7.validate_stored_release_m7(report, directory), evidence)
            self.assertEqual(before, self._snapshot(directory),
                             "validation retouched the historical CRLF evidence bytes/mtimes")

    def test_stored_content_tamper_is_rejected_without_rewrite(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report, evidence, path = self._fixture(directory)
            stored = json.loads(path.read_text(encoding="utf-8-sig"))
            stored["realtime_official_client"]["tests"] += 1
            path.write_bytes((json.dumps(stored, indent=2, sort_keys=True) + "\n").encode("utf-8"))
            before = self._snapshot(directory)
            with self.assertRaises(ValueError):
                m7.validate_stored_release_m7(report, directory)
            self.assertEqual(before, self._snapshot(directory),
                             "rejected validation rewrote the stored evidence bytes/mtimes")

    def test_recorded_hash_drift_is_rejected_without_rewrite(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report, evidence, path = self._fixture(directory)
            report["artifacts"][m7_index.RELEASE_EVIDENCE_FILE] = "0" * 64
            before = self._snapshot(directory)
            with self.assertRaises(ValueError):
                m7.validate_stored_release_m7(report, directory)
            self.assertEqual(before, self._snapshot(directory),
                             "rejected validation rewrote the stored evidence bytes/mtimes")

    def test_missing_stored_artifact_fails_without_emitting(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report, evidence, path = self._fixture(directory)
            path.unlink()
            before = self._snapshot(directory)
            with self.assertRaises(OSError):
                m7.validate_stored_release_m7(report, directory)
            self.assertFalse(path.exists())
            self.assertEqual(before, self._snapshot(directory),
                             "failed validation re-emitted the missing evidence artifact")

    def test_pure_builder_writes_nothing(self):
        with TemporaryDirectory() as temporary:
            directory = Path(temporary)
            report, evidence, path = self._fixture(directory)
            before = self._snapshot(directory)
            self.assertEqual(m7.build_release_m7(report, directory), evidence)
            self.assertEqual(before, self._snapshot(directory),
                             "the pure derivation wrote to the report directory")


if __name__ == "__main__":
    unittest.main()
