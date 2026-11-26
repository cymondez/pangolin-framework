"""Unit tests for the specification 13.6 per-profile evidence index.

The tests run entirely against a temporary report directory with a small
explicit fixture for the milestone mappings, the named-subtest groups and the
profile plans, so every negative contract (missing/changed/skipped logs,
duplicate mapping, N/A preservation, path escape, provenance drift) is proven
without touching the production contract files.
"""
import json
from contextlib import ExitStack
from pathlib import Path
from tempfile import TemporaryDirectory
import sys
from unittest import mock, TestCase

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import m0
import m6_index
import release_manifest

COMMIT = "a" * 40
SOURCE = "b" * 64
NA_REASON = "§13.4: tenant-only plan excludes organization-owned data"
MAPPED_REASON = "§13.4: applicable named subtest for this profile"
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
]}
NAMES = ("A01/one", "A01/two", "A45/x", "A67/gen", "A72/drill")
REQUIREMENTS = {"A01/one": "MOD-001", "A01/two": "MOD-001", "A45/x": "SEC-045", "A67/gen": "CLI-001", "A72/drill": "REL-005"}
PROFILES = ("tenant-only", "organization-enabled", "replacement-contract")


def fixture_mappings():
    mappings = {number: {"milestone": "M" + str(number), "baseline": "v1.3"} for number in range(1, 7)}
    mappings[1]["go_tests"] = {"A01/one": "pkg:TestOne", "A01/two": "pkg:TestTwo"}
    mappings[1]["python_tests"] = {"A45/x": "tests.test_fixture.Engineering.test_x"}
    mappings[5]["template_tests"] = {"A67/gen": m6_index.TEMPLATE_CONTRACT}
    mappings[6]["drills"] = {"A72/drill": "clean-rebuild"}
    return mappings


def fixture_profiles():
    manifests = {}
    for name in PROFILES:
        rows = []
        for ident in NAMES:
            applicability = "required"
            reason = MAPPED_REASON
            if name == "tenant-only" and ident == "A01/two":
                applicability = "not-applicable"
                reason = NA_REASON
            rows.append({"id": ident, "requirement": REQUIREMENTS[ident], "applicability": applicability,
                         "reason": reason, "evidence": "artifacts/{commit}/{profile}/" + ident + "/"})
        manifests[name] = {"profile": name, "baseline": "v1.3", "status": "planned", "subtests": rows}
    return list(zip(PROFILES, [manifests[name] for name in PROFILES]))


class IndexTestCase(TestCase):
    maxDiff = None

    def setUp(self):
        self._temporary = TemporaryDirectory()
        self.addCleanup(self._temporary.cleanup)
        self.directory = Path(self._temporary.name)
        self.hashes = {}
        self._write("01-unit.log", "\n".join([
            json.dumps({"Action": "run", "Package": "pkg"}),
            json.dumps({"Action": "pass", "Package": "pkg", "Test": "TestOne"}),
            json.dumps({"Action": "pass", "Package": "pkg", "Test": "TestTwo"})]) + "\n")
        self._write("02-integration.log", "\n".join([
            json.dumps({"Action": "run", "Package": "pkg"}),
            json.dumps({"Action": "pass", "Package": "pkg", "Test": "TestOne"})]) + "\n")
        self._write("03-m1-regression.log",
                    "test_x (tests.test_fixture.Engineering.test_x) ... ok\nRan 1 test in 0.001s\n\nOK\n")
        self._write("04-templates.log", "Both generated Bookstore profiles passed\n")
        self._write("templates.json", json.dumps({"status": "passed", "profiles": [
            {"profile": "tenant-only", "build_test_start": True},
            {"profile": "organization-enabled", "build_test_start": True}]}))
        (self.directory / "release-drills").mkdir()
        self._write(str(Path("release-drills") / "release-drills.json"), json.dumps({
            "status": "passed", "commit": COMMIT, "source_sha256": SOURCE, "platform": "windows-amd64",
            "project": "pangolin-m6-fixture",
            "drills": {"clean-rebuild": {"status": "passed", "commands": []}}}))
        self.report = {
            "commit": COMMIT, "source_sha256": SOURCE, "baseline": "v1.3", "status": "passed",
            "working_tree_dirty": False, "platform": {"GOOS": "windows", "GOARCH": "amd64"},
            "commands": [
                {"name": "unit", "exit_code": 0, "log": "01-unit.log", "sha256": self.hashes["01-unit.log"]},
                {"name": "integration", "exit_code": 0, "log": "02-integration.log", "sha256": self.hashes["02-integration.log"]},
                {"name": "m1-regression", "exit_code": 0, "log": "03-m1-regression.log", "sha256": self.hashes["03-m1-regression.log"]},
                {"name": "templates", "exit_code": 0, "log": "04-templates.log", "sha256": self.hashes["04-templates.log"]},
            ],
            "artifacts": {},
        }

    def tearDown(self):
        pass

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

    def _patched(self, mappings=None):
        stack = ExitStack()
        self.addCleanup(stack.close)
        stack.enter_context(mock.patch.object(m6_index, "NAMED_SUBTESTS", len(NAMES)))
        stack.enter_context(mock.patch.object(m6_index, "_mappings", lambda root: mappings or fixture_mappings()))
        stack.enter_context(mock.patch.object(m6_index, "_subtest_groups", lambda root: GROUPS["groups"]))
        stack.enter_context(mock.patch.object(m6_index, "_profiles", lambda root, groups: fixture_profiles()))
        return stack

    def _index(self, report=None, mappings=None):
        self._patched(mappings)
        return m6_index.build_index(report or self.report, self.directory, root=ROOT)

    def _write_index(self):
        self._patched()
        return m6_index.write_index(self.report, self.directory, root=ROOT)

    def _row(self, index, subtest, profile):
        return next(row for row in index["rows"] if row["subtest"] == subtest)["profiles"][profile]

    def test_required_rows_link_exact_raw_proof(self):
        index = self._index()
        self.assertEqual(index["totals"], {"subtests": 5, "rows": 15, "required_rows": 14, "not_applicable_rows": 1})
        evidence = self._row(index, "A01/one", "tenant-only")["evidence"]
        self.assertEqual(evidence["identity"], "pkg:TestOne")
        self.assertEqual(evidence["kind"], "go-test")
        self.assertEqual(evidence["log"], "01-unit.log")
        self.assertEqual(evidence["sha256"], m0.digest(self.directory / "01-unit.log"))
        self.assertTrue(evidence["shared_contract"])
        self.assertEqual(evidence["linked_profiles"], sorted(PROFILES))
        python = self._row(index, "A45/x", "replacement-contract")["evidence"]
        self.assertEqual(python["kind"], "python-regression")
        self.assertEqual(python["identity"], "tests.test_fixture.Engineering.test_x")
        template = self._row(index, "A67/gen", "tenant-only")["evidence"]
        self.assertEqual(template["kind"], "template")
        self.assertEqual(template["build_test_start_profiles"], ["organization-enabled", "tenant-only"])
        drill = self._row(index, "A72/drill", "organization-enabled")["evidence"]
        self.assertEqual(drill["entry"], "clean-rebuild")
        self.assertEqual(drill["sha256"], m0.digest(self.directory / "release-drills" / "release-drills.json"))
        written = self._write_index()
        self.assertEqual(written, index)
        target = self.directory / m6_index.EVIDENCE_FILE
        self.assertEqual(json.loads(target.read_text(encoding="utf-8")), index)

    def test_artifacts_dict_may_be_absent_or_populated(self):
        self.assertEqual(self._index()["totals"]["subtests"], 5)
        self.report["artifacts"] = {
            "01-unit.log": m0.digest(self.directory / "01-unit.log"),
            "release-drills\\release-drills.json": m0.digest(self.directory / "release-drills" / "release-drills.json")}
        self.assertEqual(self._index()["totals"]["subtests"], 5)
        self.report["artifacts"] = {"01-unit.log": "0" * 64}
        with self.assertRaises(ValueError):
            self._index()

    def test_missing_log_file_is_rejected(self):
        (self.directory / "01-unit.log").unlink()
        with self.assertRaises(ValueError):
            self._index()

    def test_missing_required_case_is_rejected(self):
        self._write("01-unit.log", "\n".join([
            json.dumps({"Action": "pass", "Package": "pkg", "Test": "TestOne"})]) + "\n")
        self._sync_hashes()
        with self.assertRaises(ValueError):
            self._index()

    def test_changed_log_is_rejected(self):
        path = self.directory / "01-unit.log"
        path.write_text(json.dumps({"Action": "pass", "Package": "pkg", "Test": "TestOne"}) + "\n", encoding="utf-8")
        self.assertNotEqual(m0.digest(path), self.hashes["01-unit.log"])
        with self.assertRaises(ValueError):
            self._index()

    def test_skipped_test_log_is_rejected(self):
        self._write("01-unit.log", "\n".join([
            json.dumps({"Action": "run", "Package": "pkg"}),
            json.dumps({"Action": "skip", "Package": "pkg", "Test": "TestOne"}),
            json.dumps({"Action": "pass", "Package": "pkg", "Test": "TestTwo"})]) + "\n")
        self._sync_hashes()
        with self.assertRaises(ValueError):
            self._index()

    def test_duplicate_mapping_across_milestones_is_rejected(self):
        mappings = fixture_mappings()
        mappings[2]["unit_tests"] = {"A01/one": "other:Test"}
        with self.assertRaises(ValueError):
            self._index(mappings=mappings)

    def test_not_applicable_row_preserves_reason_without_pass_claim(self):
        index = self._index()
        row = self._row(index, "A01/two", "tenant-only")
        self.assertEqual(row["applicability"], "not-applicable")
        self.assertEqual(row["reason"], NA_REASON)
        self.assertIsNone(row["evidence"])
        self.assertNotIn("status", row)
        self.assertNotIn("passed", row)
        shared = self._row(index, "A01/two", "organization-enabled")["evidence"]
        self.assertEqual(shared["identity"], "pkg:TestTwo")

    def test_development_reports_are_never_labelled_verified(self):
        self.assertEqual(self._index()["evidence_class"], "evidence-ready")
        self.report["working_tree_dirty"] = True
        self.assertEqual(self._index()["evidence_class"], "development-only")
        self.report["working_tree_dirty"] = False
        self.report["gate"] = "development-only"
        self.assertEqual(self._index()["evidence_class"], "development-only")
        self.report.pop("gate")
        self.report["platform"] = {"GOOS": "linux", "GOARCH": "amd64"}
        self.assertEqual(self._index()["evidence_class"], "development-only")
        self.assertIn(self._index()["evidence_class"], {"development-only", "evidence-ready"})
        self.report["gate"] = "evidence-ready"
        self.report["status"] = "failed"
        self.assertEqual(self._index()["evidence_class"], "development-only")

    def test_log_path_escape_is_rejected(self):
        self.report["commands"][0]["log"] = "..\\escape.log"
        with self.assertRaises(ValueError):
            self._index()

    def test_drill_provenance_and_status_are_bound(self):
        path = self.directory / "release-drills" / "release-drills.json"
        data = m0.load(path)
        data["commit"] = "9" * 40
        path.write_text(json.dumps(data), encoding="utf-8")
        with self.assertRaises(ValueError):
            self._index()
        data["commit"] = COMMIT
        data["drills"]["clean-rebuild"]["status"] = "failed"
        path.write_text(json.dumps(data), encoding="utf-8")
        with self.assertRaises(ValueError):
            self._index()

    def test_unknown_template_contract_is_rejected(self):
        mappings = fixture_mappings()
        mappings[5]["template_tests"] = {"A67/gen": "single-profile-build"}
        with self.assertRaises(ValueError):
            self._index(mappings=mappings)


class ClosureTestCase(TestCase):
    maxDiff = None

    def test_production_mappings_map_exactly_532_subtests(self):
        groups = m6_index._subtest_groups(m6_index.ROOT)
        closure = release_manifest.check_closure(m6_index._mappings(m6_index.ROOT), groups)
        self.assertEqual(closure["mapped"], 532)
        self.assertEqual(closure["milestones"], {"M1": 147, "M2": 134, "M3": 185, "M4": 28, "M5": 25, "M6": 13})
        self.assertEqual(sum(1 for _ in (subtest for group in groups for subtest in group["subtests"])), 532)

    def test_builder_rejects_duplicate_production_mapping(self):
        root = Path(m6_index.ROOT)
        mappings = m6_index._mappings(root)
        groups = m6_index._subtest_groups(root)
        mappings[2]["unit_tests"] = {"A01/duplicate-name": "other:Test"}
        with mock.patch.object(m6_index, "_mappings", lambda _root: mappings), \
             mock.patch.object(m6_index, "_subtest_groups", lambda _root: groups):
            with self.assertRaises(ValueError):
                m6_index.build_index({"commit": "a" * 40, "source_sha256": "b" * 64, "baseline": "v1.3"}, root)


if __name__ == "__main__":
    raise SystemExit(__import__("unittest").main())
