"""M3 evidence mutation checks; metadata cannot replace runtime acceptance."""
import copy
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import contracts
import nats_fixture
import test_runner
import verify


class M3Gates(unittest.TestCase):
    def test_complete_mapping_and_module_docs(self):
        mapping = contracts.load(ROOT / "tests/contract/m3.json")
        verify.validate_mapping(mapping, milestone="M3")
        test_runner.check_module_docs(mapping)
        self.assertEqual(sum(len(mapping[key]) for key in ("unit_tests", "integration_tests", "python_tests")), 185)

    def test_missing_duplicate_empty_and_pending_are_rejected(self):
        original = contracts.load(ROOT / "tests/contract/m3.json")
        key = next(iter(original["unit_tests"]))
        altered = copy.deepcopy(original)
        del altered["unit_tests"][key]
        with self.assertRaises(ValueError):
            verify.validate_mapping(altered, milestone="M3")
        altered = copy.deepcopy(original)
        altered["integration_tests"][key] = "duplicate"
        with self.assertRaises(ValueError):
            verify.validate_mapping(altered, milestone="M3")
        altered = copy.deepcopy(original)
        altered["unit_tests"][key] = ""
        with self.assertRaises(ValueError):
            verify.validate_mapping(altered, milestone="M3")
        altered = copy.deepcopy(original)
        altered["pending"] = [key]
        with self.assertRaises(ValueError):
            verify.validate_mapping(altered, milestone="M3")

    def test_runtime_skip_and_missing_execution_fail(self):
        passed = json.dumps({"Action": "pass", "Package": "pkg", "Test": "TestM3"})
        self.assertEqual(test_runner.validate_tests(passed, ["pkg:TestM3"]), ["pkg:TestM3"])
        for text in (passed.replace("pass", "skip"), passed.replace("pass", "fail"), "", passed):
            with self.assertRaises(ValueError):
                test_runner.validate_tests(text, ["pkg:Missing"])

    def test_non_m3_fixture_namespace_is_rejected(self):
        with self.assertRaises(ValueError):
            with nats_fixture.broker_restart("pangolin-docs", ROOT / "artifacts" / "must-not-run"):
                pass


if __name__ == "__main__":
    unittest.main()
