"""PostgreSQL client selection must work before integration, including old CI clients."""
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from types import SimpleNamespace
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import release_drills
import verify


class RunnerClients(unittest.TestCase):
    def test_drills_preserve_container_selection_despite_old_path_clients(self):
        import tempfile
        probe = subprocess.CompletedProcess([], 0, '{"Service":"postgres","State":"running"}\n', "")
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache") as directory:
            args = SimpleNamespace(pg_bin="", compose_project="pangolin-m6-ci", compose_files=["deploy/compose/m1.yaml"], output=Path(directory))
            with patch.dict(os.environ, {"PANGOLIN_M6_PG_MODE": "container"}, clear=True), patch.object(release_drills.shutil, "which", return_value="/old/pg-tool"), patch.object(release_drills.subprocess, "run", return_value=probe):
                hints = release_drills.resolve_pg(args)
                self.assertEqual(hints["PANGOLIN_M6_PG_MODE"], "container")
                self.assertEqual(json.loads(hints["PANGOLIN_M6_PG_CONTAINER_ARGV"])[3], "pangolin-m6-ci")

    def test_native_pinned_clients_need_no_docker_socket(self):
        version = subprocess.CompletedProcess([], 0, "pg_dump (PostgreSQL) 18.7\n", "")
        with patch.dict(os.environ, {}, clear=True), patch.object(verify.shutil, "which", return_value="/bin/pg-tool"), patch.object(verify.subprocess, "run", return_value=version):
            verify.prepare_pg_clients("pangolin-m6-test")
            self.assertEqual(os.environ["PANGOLIN_M6_PG_MODE"], "host")
            self.assertNotIn("PANGOLIN_M6_PG_CONTAINER_ARGV", os.environ)

    def test_old_ci_clients_use_only_the_owned_fixture(self):
        version = subprocess.CompletedProcess([], 0, "pg_dump (PostgreSQL) 16.9\n", "")
        with patch.dict(os.environ, {}, clear=True), patch.object(verify.shutil, "which", return_value="/bin/pg-tool"), patch.object(verify.subprocess, "run", return_value=version):
            verify.prepare_pg_clients("pangolin-m6-ci")
            prefix = json.loads(os.environ["PANGOLIN_M6_PG_CONTAINER_ARGV"])
            self.assertEqual(os.environ["PANGOLIN_M6_PG_MODE"], "container")
            self.assertEqual(prefix[:4], ["docker", "compose", "-p", "pangolin-m6-ci"])
            self.assertEqual(prefix[-3:], ["exec", "-T", "postgres"])

    def test_unowned_project_cannot_be_used_as_fallback(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(verify.shutil, "which", return_value=None):
            with self.assertRaises(ValueError):
                verify.prepare_pg_clients("pangolin-docs")

    def test_explicit_modes_fail_closed_when_incomplete(self):
        for mode in ("unknown", "container"):
            with self.subTest(mode=mode), patch.dict(os.environ, {"PANGOLIN_M6_PG_MODE": mode}, clear=True):
                with self.assertRaises(ValueError):
                    verify.prepare_pg_clients("pangolin-m6-test")


if __name__ == "__main__":
    unittest.main()
