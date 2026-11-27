"""Unified local CI verifier mirroring GitHub Actions workflows before pushing.

Runs checks on BOTH the host (Windows/Linux) AND a real case-sensitive Linux
ext4 Docker container (built from deploy/docker/Dockerfile.m7-runner, copying
source into container-local /work with Linux ext4 named volumes for GOMODCACHE
and GOCACHE so Windows NTFS bind-mount case-insensitivity cannot mask Linux bugs).

Usage:
  python scripts/local_ci.py                 # fast mode: host + Linux ext4 Docker (contracts, tests, CLI builds, docs if changed)
  python scripts/local_ci.py --mode docs     # mirror .github/workflows/docs.yml inside Linux ext4 Docker
  python scripts/local_ci.py --mode release  # mirror .github/workflows/release.yml inside Linux ext4 Docker (5 CLI targets + Pages)
  python scripts/local_ci.py --mode ci       # full mirror of ci.yml: Docker Compose + pinned Linux -race + coverage + drills + SignalR

On a clean git working tree, a passing run records a commit-bound stamp under
.cache/local-ci/<commit>.json so .githooks/pre-push allows pushing that commit.
Never use GitHub Actions as a trial-and-error debugger.
"""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time

import contracts
import test_runner

ROOT = contracts.ROOT
STAMP_DIR = ROOT / ".cache" / "local-ci"
LINUX_IMAGE = "pangolin-local-ci-runner:0.1.0"
LINUX_DOCKERFILE = "deploy/docker/Dockerfile.m7-runner"
LINUX_GOMOD_VOLUME = "pangolin-local-ci-gomod"
LINUX_GOCACHE_VOLUME = "pangolin-local-ci-gocache"
LINUX_NPM_VOLUME = "pangolin-local-ci-npm"
LIVE_SKIP_PATTERN = (
    "^(TestDurableDeliveryAcknowledge|"
    "TestLiveFanOutAckRejectAndClose|"
    "TestLiveAckWaitRedeliverySameConsumer|"
    "TestReceiverFanOutLifecycle|"
    "TestReceiverDrainDeadline|"
    "TestReceiverNoClientAckAndRedelivery|"
    "TestRealtimePublish|"
    "TestCleanupStreamLive)"
)
DOCS_WATCH_PREFIXES = (
    "docs/",
    "examples/basics/",
    "examples/moduleapp/",
    "scripts/docs_examples.py",
    "VERSION",
    ".github/workflows/docs.yml",
)
RELEASE_TARGETS = (
    ("linux", "amd64", ""),
    ("linux", "arm64", ""),
    ("darwin", "amd64", ""),
    ("darwin", "arm64", ""),
    ("windows", "amd64", ".exe"),
)
FAST_TARGETS = (
    ("linux", "amd64", ""),
    ("windows", "amd64", ".exe"),
)


def _git(*args: str, root: Path = ROOT) -> str:
    result = subprocess.run(
        ["git", "-c", "core.quotepath=false", *args],
        cwd=root,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=True,
    )
    return result.stdout.strip()


def _working_tree_clean(root: Path = ROOT) -> bool:
    return not _git("status", "--porcelain", root=root)


def _changed_files_since_upstream(root: Path = ROOT) -> list[str]:
    for base in ("@{u}", "origin/main", "main"):
        probe = subprocess.run(
            ["git", "rev-parse", "--verify", "--quiet", base],
            cwd=root,
            capture_output=True,
            text=True,
        )
        if probe.returncode == 0:
            diff = subprocess.run(
                ["git", "-c", "core.quotepath=false", "diff", "--name-only", f"{base}...HEAD"],
                cwd=root,
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
            )
            status = subprocess.run(
                ["git", "-c", "core.quotepath=false", "status", "--porcelain"],
                cwd=root,
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
            )
            files = [line.strip() for line in diff.stdout.splitlines() if line.strip()]
            for line in status.stdout.splitlines():
                if len(line) > 3:
                    files.append(line[3:].strip().split(" -> ")[-1])
            return sorted(set(files))
    return []


def docs_changed(changed_files: list[str]) -> bool:
    return any(
        path == prefix or path.startswith(prefix)
        for path in changed_files
        for prefix in DOCS_WATCH_PREFIXES
    )


def _npm_executable() -> str:
    candidates = ("npm.cmd", "npm") if os.name == "nt" else ("npm",)
    for name in candidates:
        found = shutil.which(name)
        if found:
            return found
    raise RuntimeError("npm not found in PATH; install Node.js 22.x to verify docs/site")


def _node_executable() -> str:
    found = shutil.which("node")
    if not found:
        raise RuntimeError("node not found in PATH; install Node.js 22.x to verify docs/site")
    return found


def _run_step(name: str, argv: list[str], cwd: Path = ROOT, env: dict[str, str] | None = None, capture: bool = False) -> str:
    started = time.monotonic()
    print(f"[RUN ] {name}", flush=True)
    merged_env = contracts.go_environment(ROOT)
    if env:
        merged_env.update(env)
    result = subprocess.run(
        argv,
        cwd=cwd,
        env=merged_env,
        capture_output=capture,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    elapsed = time.monotonic() - started
    if result.returncode != 0:
        if capture:
            if result.stdout:
                print(result.stdout, file=sys.stderr)
            if result.stderr:
                print(result.stderr, file=sys.stderr)
        raise RuntimeError(f"Step '{name}' failed with exit code {result.returncode} ({elapsed:.1f}s)")
    print(f"[PASS] {name} ({elapsed:.1f}s)", flush=True)
    return result.stdout if capture else ""


def run_static_contracts() -> None:
    _run_step("go mod download all", ["go", "mod", "download", "all"])
    _run_step("tools/check", ["go", "run", "./tools/check"])
    _run_step("contracts.py check", [sys.executable, "scripts/contracts.py", "check"])
    _run_step("release_manifest.py --check", [sys.executable, "scripts/release_manifest.py", "--check"])
    _run_step("template_bundle.py --check", [sys.executable, "scripts/template_bundle.py", "--check"])


def run_python_gate_tests() -> None:
    for milestone in ("m0", "m1", "m2", "m3", "m4", "m5", "m6", "m7"):
        if milestone == "m7":
            client_dir = ROOT / "examples" / "realtime" / "client"
            if not (client_dir / "node_modules").is_dir():
                _run_step(
                    "examples/realtime/client npm ci --ignore-scripts",
                    [_npm_executable(), "ci", "--ignore-scripts", "--no-audit", "--no-fund"],
                    cwd=client_dir,
                )
        _run_step(
            f"python tests/{milestone}",
            [sys.executable, "-m", "unittest", "discover", "-s", f"tests/{milestone}", "-p", "test_*.py"],
        )


def run_go_portability_checks() -> None:
    go_dirs = contracts.go_packages(ROOT)
    build_dirs = contracts.go_build_packages(ROOT)
    _run_step("go build (tracked packages)", ["go", "build", *build_dirs])
    _run_step("go vet (tracked packages)", ["go", "vet", *go_dirs])
    json_out = _run_step(
        "go test -json (service-free unit suite)",
        ["go", "test", "-count=1", "-json", "-skip", LIVE_SKIP_PATTERN, *go_dirs],
        capture=True,
    )
    passed = test_runner.validate_tests(json_out)
    print(f"[PASS] validate_tests: {len(passed)} passed, 0 failed, 0 skipped", flush=True)


def run_cli_cross_builds(targets: tuple[tuple[str, str, str], ...]) -> None:
    with tempfile.TemporaryDirectory(prefix="pangolin-cli-check-", dir=ROOT / ".cache") as tmp:
        stage = Path(tmp)
        for goos, goarch, ext in targets:
            out = stage / f"pangolin_{goos}_{goarch}{ext}"
            _run_step(
                f"build cmd/pangolin ({goos}/{goarch})",
                ["go", "build", "-trimpath", "-o", str(out), "./cmd/pangolin"],
                env={"CGO_ENABLED": "0", "GOOS": goos, "GOARCH": goarch},
            )


def run_docs_checks(include_versioning: bool = True, include_examples: bool = True) -> None:
    site = ROOT / "docs" / "site"
    npm = _npm_executable()
    node = _node_executable()
    if not (site / "node_modules").is_dir():
        _run_step("docs/site npm ci", [npm, "ci", "--no-audit", "--no-fund"], cwd=site)
    _run_step("docs/site npm test", [npm, "test"], cwd=site)
    _run_step(
        "docs/site GitHub Pages subpath build (/pangolin/)",
        [npm, "run", "build"],
        cwd=site,
        env={"DOCS_URL": "https://pangolin-framework.github.io", "DOCS_BASE_URL": "/pangolin/"},
    )
    _run_step("docs/site root build (/)", [npm, "run", "build"], cwd=site)
    if include_versioning:
        _run_step("docs/site versioning proof", [node, "scripts/check-versioning.mjs"], cwd=site)
    if include_examples:
        _run_step("docs consumer examples", [sys.executable, "scripts/docs_examples.py"])


def _ensure_linux_runner_image() -> None:
    if not shutil.which("docker"):
        raise RuntimeError("docker CLI not found; Docker Desktop is required for Linux ext4 local CI")
    _run_step(
        f"ensure pinned Linux CI image ({LINUX_IMAGE})",
        [
            "docker", "build",
            "-f", str(ROOT / LINUX_DOCKERFILE),
            "--label", "dev.pangolin.owner=pangolin-framework",
            "--label", "dev.pangolin.purpose=local-ci-runner",
            "-t", LINUX_IMAGE,
            str(ROOT),
        ],
    )
    for volume in (LINUX_GOMOD_VOLUME, LINUX_GOCACHE_VOLUME, LINUX_NPM_VOLUME):
        subprocess.run(
            ["docker", "volume", "create", "--label", "dev.pangolin.owner=pangolin-framework", "--label", "dev.pangolin.purpose=local-ci-cache", volume],
            cwd=ROOT,
            capture_output=True,
            check=True,
        )


def run_in_linux_docker(mode: str, run_docs: bool) -> None:
    """Copy tracked+untracked repo files onto container-local Linux ext4 /work and run local_ci.py inside."""
    _ensure_linux_runner_image()
    inner_mode = mode
    inner_flags = ["--in-container", "--no-stamp", "--mode", inner_mode]
    if run_docs and inner_mode == "fast":
        inner_flags.append("--docs")
    elif not run_docs and inner_mode == "fast":
        inner_flags.append("--skip-docs")
    inner_cmd = " ".join(inner_flags)
    # Copy repo into native Linux ext4 /work (excluding host caches/artifacts/node_modules)
    # and mount a Linux ext4 named volume at /work/.cache/go-mod so BOTH source files and
    # downloaded Go module files are tested on a true case-sensitive Linux filesystem.
    script = (
        "set -eu\n"
        "mkdir -p /work/.cache/go-tmp\n"
        "tar -C /source "
        "--exclude='./.cache' --exclude='./artifacts' --exclude='./%TEMP%' "
        "--exclude='./docs/site/node_modules' --exclude='./docs/site/build' --exclude='./docs/site/.docusaurus' --exclude='./docs/site/.cache' "
        "--exclude='./examples/realtime/client/node_modules' "
        "-cf - . | tar -C /work -xf -\n"
        "cd /work\n"
        "git config --global --add safe.directory /work\n"
        "mkdir -p /work/.cache/go-mod /work/.cache/go-tmp /cache/go-build /cache/npm\n"
        "export GOCACHE=/cache/go-build\n"
        "export npm_config_cache=/cache/npm\n"
        f"python3 scripts/local_ci.py {inner_cmd}\n"
    )
    _run_step(
        f"Linux ext4 Docker CI ({mode})",
        [
            "docker", "run", "--rm",
            "--label", "dev.pangolin.owner=pangolin-framework",
            "--label", "dev.pangolin.purpose=local-ci-run",
            "-v", f"{ROOT}:/source:ro",
            "-v", f"{LINUX_GOMOD_VOLUME}:/work/.cache/go-mod",
            "-v", f"{LINUX_GOCACHE_VOLUME}:/cache/go-build",
            "-v", f"{LINUX_NPM_VOLUME}:/cache/npm",
            LINUX_IMAGE,
            "sh", "-c", script,
        ],
    )


def run_docker_ci(project: str, scope: str, race: bool, linux_container: bool = True) -> None:
    if not re.fullmatch(r"pangolin-(?:m[67]|release|realtime)-[a-z0-9-]+", project):
        raise ValueError("Docker CI project must match pangolin-(m6|m7|release|realtime)-*")
    compose = [
        "docker", "compose", "-p", project,
        "-f", "deploy/compose/m1.yaml",
        "-f", "deploy/compose/m5-databases.yaml",
    ]
    linux_sqlc = ROOT / ".cache" / "linux-bin" / "sqlc"
    if linux_container and not linux_sqlc.is_file():
        _ensure_linux_runner_image()
        linux_sqlc.parent.mkdir(parents=True, exist_ok=True)
        _run_step(
            "install Linux sqlc v1.31.1 inside Linux container",
            [
                "docker", "run", "--rm",
                "-v", f"{linux_sqlc.parent}:/out",
                "-v", f"{LINUX_GOMOD_VOLUME}:/gomod",
                "-v", f"{LINUX_GOCACHE_VOLUME}:/gocache",
                "-e", "GOBIN=/out",
                "-e", "GOMODCACHE=/gomod",
                "-e", "GOCACHE=/gocache",
                LINUX_IMAGE,
                "go", "install", "github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1",
            ],
        )
    _run_step(
        "build Linux test-idp fixture",
        ["go", "build", "-o", "artifacts/test-idp", "./internal/testkit/cmd/idp"],
        env={"CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "amd64"},
    )
    _run_step("docker compose up (isolated fixture)", [*compose, "up", "-d", "--build", "--wait", "--wait-timeout", "120"])
    fixture_env = {
        "PANGOLIN_PROJECT": project,
        "PANGOLIN_SCOPE": scope,
        "PANGOLIN_M5_COMPOSE": "1",
        "PANGOLIN_M7_DEMO_KEY": "local-ci-realtime-demo-key",
        "PANGOLIN_M1_ADMIN_DSN": "postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15542/pangolin_m0?sslmode=disable",
        "PANGOLIN_M1_APP_DSN": "postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable",
        "PANGOLIN_M1_WORKER_DSN": "postgres://pangolin_worker:local-worker-only@127.0.0.1:15542/pangolin_m0?sslmode=disable",
        "PANGOLIN_M1_NATS_URL": "nats://127.0.0.1:14322",
        "PANGOLIN_M1_ISSUER": "http://127.0.0.1:18180",
    }
    try:
        if linux_container:
            drills_dir = ROOT / "artifacts" / f"local-ci-drills-{project}"
            if drills_dir.exists():
                shutil.rmtree(drills_dir)
            _run_step(
                "host release drills (for Linux container adoption)",
                [sys.executable, "scripts/release_drills.py", "--output", str(drills_dir), "--project", project],
                env=fixture_env,
            )
            _run_step(
                f"pinned Linux/amd64 OCI -race gate ({scope})",
                [sys.executable, "scripts/linux_runner.py", "--project", project, "--target", scope],
                env={**fixture_env, "PANGOLIN_M6_DRILLS_DIR": str(drills_dir)},
            )
        else:
            verify_argv = [sys.executable, "scripts/verify.py", "verify", "--project", project, "--scope", scope]
            if race:
                verify_argv.append("--race")
            _run_step(f"verify.py ({scope})", verify_argv, env=fixture_env)
    finally:
        subprocess.run([*compose, "down", "--volumes", "--rmi", "local"], cwd=ROOT, check=False)


def stamp_path(commit: str, root: Path = ROOT) -> Path:
    return root / ".cache" / "local-ci" / f"{commit}.json"


def record_stamp(commit: str, mode: str, docs_verified: bool, linux_docker: bool, root: Path = ROOT) -> Path:
    path = stamp_path(commit, root)
    path.parent.mkdir(parents=True, exist_ok=True)
    data = {"commit": commit, "modes": [], "docs_verified": False, "linux_docker": False}
    if path.is_file():
        try:
            existing = json.loads(path.read_text(encoding="utf-8"))
            if existing.get("commit") == commit:
                data = existing
        except (ValueError, OSError):
            pass
    modes = sorted(set(data.get("modes", [])) | {mode})
    data.update(
        commit=commit,
        modes=modes,
        docs_verified=bool(data.get("docs_verified")) or docs_verified,
        linux_docker=bool(data.get("linux_docker")) or linux_docker,
        verified_at=datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    )
    path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
    return path


def check_stamp(
    commit: str,
    require_release: bool = False,
    require_docs: bool = False,
    require_linux_docker: bool = True,
    root: Path = ROOT,
) -> tuple[bool, str]:
    path = stamp_path(commit, root)
    if not path.is_file():
        return False, f"no local CI pass stamp for commit {commit[:7]}"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (ValueError, OSError) as error:
        return False, f"unreadable local CI stamp: {error}"
    if data.get("commit") != commit:
        return False, f"stamp commit mismatch for {commit[:7]}"
    modes = set(data.get("modes") or [])
    if require_linux_docker and not data.get("linux_docker"):
        return False, f"Linux ext4 Docker verification required on {commit[:7]} (do not pass --no-docker before pushing)"
    if require_release and "release" not in modes:
        return False, f"tag push requires 'python scripts/local_ci.py --mode release' on {commit[:7]} (recorded modes: {sorted(modes)})"
    if require_docs and not data.get("docs_verified"):
        return False, f"docs changes require docs verification on {commit[:7]} (run 'python scripts/local_ci.py --mode docs' or '--docs')"
    if not modes:
        return False, f"stamp for {commit[:7]} records no completed modes"
    return True, f"verified modes={sorted(modes)} docs={bool(data.get('docs_verified'))} linux_docker={bool(data.get('linux_docker'))}"


def _verify_prepush_stdin(root: Path = ROOT) -> int:
    if os.getenv("PANGOLIN_SKIP_PREPUSH") == "1":
        print("[WARN] PANGOLIN_SKIP_PREPUSH=1 set; skipping local CI pre-push guard.", file=sys.stderr)
        return 0
    zero_sha = "0" * 40
    lines = [line.strip() for line in sys.stdin.read().splitlines() if line.strip()]
    if not lines:
        return 0
    changed = _changed_files_since_upstream(root)
    needs_docs = docs_changed(changed)
    for line in lines:
        parts = line.split()
        if len(parts) < 4:
            continue
        local_ref, local_sha, remote_ref, _ = parts[:4]
        if local_sha == zero_sha:
            continue  # deleting remote branch/tag
        is_release_tag = remote_ref.startswith("refs/tags/v")
        target_commit = _git("rev-parse", f"{local_sha}^{{commit}}", root=root)
        ok, reason = check_stamp(
            target_commit,
            require_release=is_release_tag,
            require_docs=needs_docs or is_release_tag,
            require_linux_docker=True,
            root=root,
        )
        if not ok:
            print(f"[BLOCKED] Pre-push guard rejected {local_ref} -> {remote_ref}: {reason}", file=sys.stderr)
            print("[HINT   ] Run local CI (with Linux ext4 Docker) on the clean commit BEFORE pushing:", file=sys.stderr)
            if is_release_tag:
                print("          python scripts/local_ci.py --mode release", file=sys.stderr)
            else:
                print("          python scripts/local_ci.py --mode fast", file=sys.stderr)
            print("[POLICY ] Never use GitHub Actions as a trial-and-error debugger.", file=sys.stderr)
            return 1
        print(f"[OK] Pre-push local CI stamp valid for {target_commit[:7]} ({reason})", file=sys.stderr)
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("fast", "docs", "release", "ci"), default="fast")
    parser.add_argument("--docs", action="store_true", help="force running Docusaurus and consumer example checks in fast/ci mode")
    parser.add_argument("--skip-docs", action="store_true", help="skip docs checks even if docs files changed (not allowed in docs/release mode)")
    parser.add_argument("--no-docker", action="store_true", help="host-only quick check (cannot satisfy Linux ext4 pre-push stamp)")
    parser.add_argument("--host-only-ci", action="store_true", help="with --mode ci, run verify.py on host instead of inside Linux OCI container")
    parser.add_argument("--project", default="pangolin-release-local", help="isolated Compose project name for --mode ci")
    parser.add_argument("--scope", choices=("release", "realtime"), default="realtime", help="verification scope for --mode ci")
    parser.add_argument("--race", action="store_true", help="enable -race during --mode ci --host-only-ci")
    parser.add_argument("--no-stamp", action="store_true", help="do not write .cache/local-ci/<sha>.json stamp")
    parser.add_argument("--in-container", action="store_true", help=argparse.SUPPRESS)
    parser.add_argument("--check-prepush", action="store_true", help="internal entrypoint invoked by .githooks/pre-push")
    args = parser.parse_args(argv)

    if args.check_prepush:
        return _verify_prepush_stdin(ROOT)

    started = time.monotonic()
    docs_verified = False
    linux_docker_verified = False
    try:
        if args.in_container:
            if args.mode == "docs":
                run_docs_checks(include_versioning=True, include_examples=True)
                docs_verified = True
            elif args.mode == "release":
                run_static_contracts()
                run_python_gate_tests()
                run_go_portability_checks()
                run_cli_cross_builds(RELEASE_TARGETS)
                run_docs_checks(include_versioning=True, include_examples=True)
                docs_verified = True
            else:
                run_static_contracts()
                run_python_gate_tests()
                run_go_portability_checks()
                run_cli_cross_builds(FAST_TARGETS)
                if args.docs:
                    run_docs_checks(include_versioning=True, include_examples=True)
                    docs_verified = True
        elif args.mode == "docs":
            if args.no_docker:
                run_docs_checks(include_versioning=True, include_examples=True)
            else:
                run_in_linux_docker("docs", run_docs=True)
                linux_docker_verified = True
            docs_verified = True
        elif args.mode == "release":
            run_go_portability_checks()
            if args.no_docker:
                run_static_contracts()
                run_python_gate_tests()
                run_cli_cross_builds(RELEASE_TARGETS)
                run_docs_checks(include_versioning=True, include_examples=True)
            else:
                run_in_linux_docker("release", run_docs=True)
                linux_docker_verified = True
            docs_verified = True
        elif args.mode == "ci":
            run_go_portability_checks()
            run_docker_ci(args.project, args.scope, args.race, linux_container=not args.host_only_ci)
            linux_docker_verified = not args.host_only_ci
            should_run_docs = args.docs or (not args.skip_docs and docs_changed(_changed_files_since_upstream(ROOT)))
            if should_run_docs:
                run_in_linux_docker("docs", run_docs=True)
                docs_verified = True
        else:  # fast
            should_run_docs = args.docs or (not args.skip_docs and docs_changed(_changed_files_since_upstream(ROOT)))
            run_go_portability_checks()
            if args.no_docker:
                run_static_contracts()
                run_python_gate_tests()
                run_cli_cross_builds(FAST_TARGETS)
                if should_run_docs:
                    run_docs_checks(include_versioning=True, include_examples=True)
                    docs_verified = True
            else:
                run_in_linux_docker("fast", run_docs=should_run_docs)
                linux_docker_verified = True
                docs_verified = should_run_docs
    except (RuntimeError, ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f"[FAIL] Local CI ({args.mode}) failed: {error}", file=sys.stderr)
        return 1

    elapsed = time.monotonic() - started
    if args.in_container:
        print(f"[PASS] In-container Linux ext4 CI ({args.mode}) passed in {elapsed:.1f}s.", flush=True)
        return 0
    commit = _git("rev-parse", "HEAD")
    clean = _working_tree_clean()
    if not args.no_stamp and clean:
        path = record_stamp(commit, args.mode, docs_verified, linux_docker_verified)
        print(
            f"[PASS] Local CI ({args.mode}) passed in {elapsed:.1f}s; "
            f"recorded stamp {path.relative_to(ROOT)} (linux_docker={linux_docker_verified})",
            flush=True,
        )
    elif not clean:
        print(
            f"[PASS] Local CI ({args.mode}) passed in {elapsed:.1f}s "
            "(working tree dirty: commit first and re-run to record a pre-push stamp).",
            flush=True,
        )
    else:
        print(f"[PASS] Local CI ({args.mode}) passed in {elapsed:.1f}s.", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
