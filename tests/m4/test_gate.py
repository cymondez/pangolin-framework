"""M4 gate must reject incomplete maps, skips and missing load evidence."""
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


class M4Gates(unittest.TestCase):
    def test_mapping_and_documentation(self):
        mapping = contracts.load(ROOT / "tests/contract/m4.json")
        verify.validate_mapping(mapping, milestone="M4")
        test_runner.check_module_docs(mapping)
        self.assertEqual(sum(len(mapping[k]) for k in ("unit_tests", "integration_tests", "python_tests")), 28)

    def test_missing_duplicate_pending_and_blank(self):
        original = contracts.load(ROOT / "tests/contract/m4.json")
        key = next(iter(original["unit_tests"]))
        for mode in ("missing", "duplicate", "pending", "blank"):
            altered = copy.deepcopy(original)
            if mode == "missing":
                del altered["unit_tests"][key]
            elif mode == "duplicate":
                altered["integration_tests"][key] = "duplicate"
            elif mode == "pending":
                altered["pending"] = [key]
            else:
                altered["unit_tests"][key] = ""
            with self.assertRaises(ValueError):
                verify.validate_mapping(altered, milestone="M4")

    def test_skip_and_missing_case(self):
        for event in ({"Action": "skip", "Test": "T"}, {"Action": "fail"}, {"Action": "pass", "Package": "p", "Test": "other"}):
            with self.assertRaises(ValueError):
                test_runner.validate_tests(json.dumps(event), ["p:T"])

    def test_fixture_rejects_unowned_project(self):
        for project in ("pangolin-docs", "pangolin-m3-dev"):
            with self.assertRaises(ValueError):
                with nats_fixture.broker_restart(project, ROOT / "artifacts" / "never-run", milestone=4):
                    pass


if __name__ == "__main__":
    unittest.main()
