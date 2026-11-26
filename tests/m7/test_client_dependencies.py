"""Fail-closed regressions for the M7 SignalR client dependency/license inventory.

The validator derives the exact installed npm inventory from package-lock.json
plus node_modules and compares it to the reviewed immutable manifest. These
tests prove the happy path on a small hermetic fixture, every demanded failure
(tampered version, license metadata, integrity, license-file content drift,
missing installed package or license file, lock drift, missing or unexpected
dependency, unknown license) and the actual current client inventory checked
into this repository.
"""
import hashlib
import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import m7_client_dependencies as module

LOCK = {
    "lockfileVersion": 3,
    "requires": True,
    "packages": {
        "": {"name": "fixture-client", "dependencies": {"alpha": "1.0.0", "beta": "1.0.0"}},
        "node_modules/alpha": {"version": "1.0.0", "resolved": "https://registry.npmjs.org/alpha/-/alpha-1.0.0.tgz",
                               "integrity": "sha512-alphaIntegrityHash", "license": "MIT"},
        "node_modules/beta": {"version": "2.1.0", "resolved": "https://registry.npmjs.org/beta/-/beta-2.1.0.tgz",
                              "integrity": "sha512-betaIntegrityHash", "license": "BSD-3-Clause"},
    },
}


def build_fixture(root):
    client = root / "client"
    for name, version, license in (("alpha", "1.0.0", "MIT"), ("beta", "2.1.0", "BSD-3-Clause")):
        directory = client / "node_modules" / name
        directory.mkdir(parents=True)
        (directory / "package.json").write_text(json.dumps({"name": name, "version": version, "license": license}), encoding="utf-8")
        (directory / "LICENSE").write_text("Fixture license text of " + name + "\n", encoding="utf-8")
    (client / "package.json").write_text(json.dumps({"name": "fixture-client", "dependencies": {"alpha": "1.0.0", "beta": "1.0.0"}}), encoding="utf-8")
    (client / "package-lock.json").write_text(json.dumps(LOCK, indent=2) + "\n", encoding="utf-8")
    return client


def reviewed_manifest(client):
    actual = module.inventory(client)
    packages = {name: {key: value for key, value in entry.items() if key != "installed_version"}
                for name, entry in actual["packages"].items()}
    return {"reviewed_at": "2026-11-26", **{key: value for key, value in actual.items() if key != "packages"},
            "allowed_licenses": ["BSD-3-Clause", "MIT"], "packages": packages}


class ClientDependenciesTest(unittest.TestCase):
    def fails(self, action, message):
        with self.assertRaises(ValueError) as raised:
            action()
        self.assertIn(message, str(raised.exception))

    def test_fixture_inventory_matches_reviewed_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            manifest_path = Path(tmp) / "manifest.json"
            manifest_path.write_text(json.dumps(reviewed_manifest(client), indent=2), encoding="utf-8")
            summary = module.check(client, manifest_path)
            self.assertEqual(summary["status"], "ok")
            self.assertEqual(summary["locked_packages"], 2)
            self.assertEqual(summary["client_root"].replace("\\", "/"), str(client).replace("\\", "/"))

    def test_tampered_installed_version_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            package = client / "node_modules/alpha/package.json"
            package.write_text(json.dumps({"name": "alpha", "version": "9.9.9", "license": "MIT"}), encoding="utf-8")
            self.fails(lambda: module.inventory(client), "installed version mismatch: alpha installs 9.9.9 but lock pins 1.0.0")

    def test_tampered_installed_license_metadata_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            package = client / "node_modules/beta/package.json"
            package.write_text(json.dumps({"name": "beta", "version": "2.1.0", "license": "MIT"}), encoding="utf-8")
            self.fails(lambda: module.inventory(client), "installed license metadata mismatch: beta")

    def test_missing_installed_package_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            shutil.rmtree(client / "node_modules/beta")
            self.fails(lambda: module.inventory(client), "installed package missing: beta")

    def test_missing_integrity_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            lock = json.loads((client / "package-lock.json").read_text(encoding="utf-8"))
            del lock["packages"]["node_modules/alpha"]["integrity"]
            (client / "package-lock.json").write_text(json.dumps(lock, indent=2), encoding="utf-8")
            self.fails(lambda: module.inventory(client), "missing integrity: alpha")

    def test_license_file_content_drift_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            manifest = reviewed_manifest(client)
            with (client / "node_modules/alpha/LICENSE").open("a", encoding="utf-8") as license_file:
                license_file.write("Tampered extra clause\n")
            self.fails(lambda: module.validate(manifest, module.inventory(client)), "license-file content drift: alpha")

    def test_missing_license_file_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            manifest = reviewed_manifest(client)
            (client / "node_modules/beta/LICENSE").unlink()
            self.fails(lambda: module.validate(manifest, module.inventory(client)), "license-file content drift: beta")

    def test_lock_drift_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            manifest = reviewed_manifest(client)
            lock = json.loads((client / "package-lock.json").read_text(encoding="utf-8"))
            lock["packages"]["node_modules/alpha"]["funding"] = {"url": "https://example.com/sponsor"}
            (client / "package-lock.json").write_text(json.dumps(lock, indent=2), encoding="utf-8")
            self.fails(lambda: module.validate(manifest, module.inventory(client)), "lock drift")

    def test_missing_dependency_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            manifest = reviewed_manifest(client)
            manifest["packages"]["gamma"] = dict(manifest["packages"]["alpha"])
            self.fails(lambda: module.validate(manifest, module.inventory(client)),
                       "dependency inventory changed; review required, missing: gamma")

    def test_unexpected_dependency_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            manifest = reviewed_manifest(client)
            del manifest["packages"]["beta"]
            self.fails(lambda: module.validate(manifest, module.inventory(client)),
                       "dependency inventory changed; review required, unexpected: beta")

    def test_integrity_drift_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            manifest = reviewed_manifest(client)
            manifest["packages"]["alpha"]["integrity"] = "sha512-tamperedIntegrityHash"
            self.fails(lambda: module.validate(manifest, module.inventory(client)), "integrity drift: alpha")

    def test_license_metadata_drift_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            manifest = reviewed_manifest(client)
            manifest["packages"]["beta"]["license"] = "MIT"
            self.fails(lambda: module.validate(manifest, module.inventory(client)), "license metadata drift: beta")

    def test_unknown_license_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = build_fixture(Path(tmp))
            manifest = reviewed_manifest(client)
            manifest["allowed_licenses"] = ["MIT"]
            self.fails(lambda: module.validate(manifest, module.inventory(client)), "unknown license 'BSD-3-Clause' for beta")

    def test_repository_inventory_matches_reviewed_manifest(self):
        summary = module.check()
        client = ROOT / "examples/realtime/client"
        self.assertEqual(summary["status"], "ok")
        self.assertEqual(summary["locked_packages"], 18)
        self.assertEqual(summary["package_json_sha256"], hashlib.sha256((client / "package.json").read_bytes()).hexdigest())
        self.assertEqual(summary["lockfile_sha256"], hashlib.sha256((client / "package-lock.json").read_bytes()).hexdigest())

    def test_repository_inventory_records_signalr_tree(self):
        actual = module.inventory()
        packages = actual["packages"]
        self.assertEqual(packages["@microsoft/signalr"], {
            "version": "10.0.0", "installed_version": "10.0.0",
            "resolved": "https://registry.npmjs.org/@microsoft/signalr/-/signalr-10.0.0.tgz",
            "integrity": "sha512-0BRqz/uCx3JdrOqiqgFhih/+hfTERaUfCZXFB52uMaZJrKaPRzHzMuqVsJC/V3pt7NozcNXGspjKiQEK+X7P2w==",
            "license": "MIT", "license_file": None, "license_sha256": None})
        self.assertEqual(packages["fetch-cookie"]["license"], "Unlicense")
        self.assertEqual(packages["fetch-cookie"]["license_file"], "UNLICENSE")
        self.assertEqual(packages["tr46"]["license_file"], None)
        self.assertEqual(packages["punycode"]["license_file"], "LICENSE-MIT.txt")
        self.assertEqual(packages["tough-cookie"]["license"], "BSD-3-Clause")
        self.assertEqual(packages["webidl-conversions"]["license"], "BSD-2-Clause")
        self.assertEqual(packages["ws"]["version"], "7.5.14")
        self.assertEqual(packages["node-fetch"]["version"], "2.7.0")
        self.assertEqual(len(packages), 18)
        for name, entry in packages.items():
            self.assertTrue(entry["resolved"].startswith("https://registry.npmjs.org/"), name)
            self.assertTrue(entry["integrity"].startswith("sha512-"), name)
            self.assertIn(entry["license"], ("BSD-2-Clause", "BSD-3-Clause", "MIT", "Unlicense"), name)
            self.assertTrue(entry["license_sha256"] is None or len(entry["license_sha256"]) == 64, name)

    def test_cli_check_passes_and_writes_gate_artifact(self):
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "m7-client-dependencies-actual.json"
            result = subprocess.run([sys.executable, "scripts/m7_client_dependencies.py", "--check", "--output", str(artifact)],
                                    cwd=ROOT, capture_output=True, text=True, encoding="utf-8")
            self.assertEqual(result.returncode, 0, result.stderr)
            summary = json.loads(result.stdout)
            self.assertEqual(summary["status"], "ok")
            self.assertEqual(summary["locked_packages"], 18)
            artifact_inventory = json.loads(artifact.read_text(encoding="utf-8"))
            self.assertEqual(artifact_inventory["packages"]["@microsoft/signalr"]["version"], "10.0.0")
            self.assertEqual(artifact_inventory["sources"]["lockfile"], "examples/realtime/client/package-lock.json")

    def test_cli_check_fails_closed_on_drifted_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            manifest = Path(tmp) / "drifted-manifest.json"
            manifest.write_text(json.dumps({**json.loads((ROOT / "deploy/release/m7-client-dependencies.json").read_text(encoding="utf-8")),
                                            "allowed_licenses": ["MIT"]}), encoding="utf-8")
            result = subprocess.run([sys.executable, "scripts/m7_client_dependencies.py", "--check", "--manifest", str(manifest)],
                                    cwd=ROOT, capture_output=True, text=True, encoding="utf-8")
            self.assertEqual(result.returncode, 1)
            self.assertIn("client dependency check failed", result.stderr)
            self.assertIn("unknown license", result.stderr)


if __name__ == "__main__":
    unittest.main()
