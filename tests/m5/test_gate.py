"""M5 gate rejects missing, skipped, stale-template and incomplete profile evidence."""
import copy
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import contracts
import e2e
import template_bundle
import template_openapi
import templates_verify
import test_runner
import verify


class M5Gates(unittest.TestCase):
    def test_mapping_and_docs(self):
        mapping = contracts.load(ROOT / "tests/contract/m5.json")
        verify.validate_mapping(mapping, milestone="M5")
        test_runner.check_module_docs(mapping)
        self.assertEqual(sum(len(mapping[k]) for k in verify.M5_FIELDS), 25)

    def test_missing_duplicate_pending_blank(self):
        original = contracts.load(ROOT / "tests/contract/m5.json")
        key = next(iter(original["unit_tests"]))
        for mode in ("missing", "duplicate", "pending", "blank"):
            mapping = copy.deepcopy(original)
            if mode == "missing":
                del mapping["unit_tests"][key]
            elif mode == "duplicate":
                mapping["template_tests"][key] = "duplicate"
            elif mode == "pending":
                mapping["pending"] = [key]
            else:
                mapping["unit_tests"][key] = ""
            with self.assertRaises(ValueError):
                verify.validate_mapping(mapping, milestone="M5")

    def test_required_skip_fails(self):
        for event in ({"Action": "skip", "Test": "T"}, {"Action": "fail"}, {"Action": "pass", "Package": "p", "Test": "other"}):
            with self.assertRaises(ValueError):
                test_runner.validate_tests(json.dumps(event), ["p:T"])

    def test_openapi_contract(self):
        for org in (False, True):
            document = template_openapi.document(org)
            templates_verify.validate_openapi(document, org)
            del document["paths"]["/books"]["post"]["requestBody"]
            with self.assertRaises(ValueError):
                templates_verify.validate_openapi(document, org)

    def test_runtime_bundle_same_source(self):
        for path, expected in template_bundle.bundle().items():
            self.assertEqual((template_bundle.DEST / path).read_bytes(), expected, path)

    def test_e2e_namespace_guard(self):
        class Accepted(Exception):
            pass

        def stop(*args):
            raise Accepted()

        with self.assertRaises(Accepted):
            e2e.run(ROOT / "artifacts", "pangolin-m5-guard", "", stop)
        with self.assertRaises(Accepted):
            e2e.run(ROOT / "artifacts", "pangolin-m6-guard", "", stop)
        with self.assertRaises(Accepted):
            e2e.run(ROOT / "artifacts", "pangolin-m7-guard", "", stop)
        for project in ("pangolin-docs", "pangolin-m8-dev"):
            with self.assertRaises(ValueError):
                e2e.run(ROOT / "artifacts", project, "", stop)

    def test_compose_project_guard_accepts_additive_milestones(self):
        for project in ("pangolin-m5-compose", "pangolin-m6-compose", "pangolin-m7-formal-x"):
            with self.subTest(project=project):
                with self.assertRaises(NotADirectoryError if sys.platform.startswith("win") else FileNotFoundError):
                    templates_verify.run_compose(ROOT / ".cache" / "m5-compose-guard", project)
        for project in ("pangolin-docs", "pangolin-m8-dev"):
            with self.subTest(project=project):
                with self.assertRaises(ValueError):
                    templates_verify.run_compose(ROOT / ".cache" / "m5-compose-guard", project)


if __name__ == "__main__":
    unittest.main()
