"""Copy exact source bytes into a Linux checkout before the cumulative gate."""
import os
from pathlib import Path
import shutil
import subprocess
import sys

original = Path("/source")
target = Path("/work/repository")
target.parent.mkdir(parents=True, exist_ok=True)
subprocess.run(["git", "-c", "safe.directory=/source", "clone", "--no-hardlinks", str(original), str(target)], check=True)
subprocess.run(["git", "-C", str(target), "config", "core.filemode", "false"], check=True)
subprocess.run(["git", "-C", str(target), "config", "core.autocrlf", "input"], check=True)
paths = subprocess.check_output(["git", "-c", "safe.directory=/source", "-C", str(original), "ls-files", "--cached", "--others", "--exclude-standard", "-z"]).decode().split("\0")
for name in filter(None, paths):
    source = original / name
    destination = target / name
    if source.is_file():
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, destination)
    elif destination.is_file():
        destination.unlink()
(target / ".cache").symlink_to("/cache", target_is_directory=True)
(target / "artifacts").symlink_to(original / "artifacts", target_is_directory=True)
# Directory-only ignore patterns do not match these Linux symlinks. Ignore
# only the runner's two links in this disposable checkout, never source files.
with (target / ".git/info/exclude").open("a") as excludes:
    excludes.write("\n/.cache\n/artifacts\n")
# Exact CRLF bytes can leave Git's stat cache dirty although the normalized
# blobs equal HEAD. Recompute tracked blobs; real source changes stay visible
# as staged differences, so dirty development runs cannot become evidence.
subprocess.run(["git", "-C", str(target), "add", "--renormalize", "--", "."], check=True)
if not Path("/cache/go-mod").exists():
    Path("/cache/go-mod").symlink_to("/gomod", target_is_directory=True)
Path("/cache/go-tmp").mkdir(parents=True, exist_ok=True)
Path("/cache/bin").mkdir(parents=True, exist_ok=True)
shutil.copyfile(original / ".cache/linux-bin/sqlc", "/cache/bin/sqlc")
Path("/cache/bin/sqlc").chmod(0o755)
sys.path.insert(0, str(target / "scripts"))
import test_runner

expected = os.environ["PANGOLIN_EXPECTED_SOURCE"]
if test_runner.source_hash() != expected:
    raise RuntimeError("Linux exact source copy mismatch")
os.chdir(target)
os.environ.update(GOCACHE="/cache/go-build", PANGOLIN_SQLC="/cache/bin/sqlc")
scope = os.environ.get("PANGOLIN_VERIFY_SCOPE", "release")
project = os.environ["PANGOLIN_VERIFY_PROJECT"]
os.execvp("python3", ["python3", "scripts/verify.py", "verify", "--project", project, "--scope", scope, "--race", "--external-broker-fixture"])
