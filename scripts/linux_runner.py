"""Run the pinned Linux/amd64 cumulative -race gate with exact host source bytes.

Usage:
  python scripts/linux_runner.py --project <name> [--target release|realtime]

Builds the digest-pinned Linux/amd64 runner image (Dockerfile.m6-runner for
release; Dockerfile.m7-runner with pinned Node.js for realtime), starts the
host-owned NATS restart barrier, mounts this repository read/write for
artifacts and runs /source/scripts/linux_entry.py inside the container.
"""
import argparse
import os
from pathlib import Path
import re
import subprocess

import contracts
import nats_fixture
import test_runner

PROJECT_RE = r"pangolin-(?:m[67]|release|realtime)-[a-z0-9-]+"


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", required=True)
    parser.add_argument("--target", choices=["release", "realtime"], default="release")
    args = parser.parse_args(argv)
    project = args.project
    if not re.fullmatch(PROJECT_RE, project):
        parser.error("dedicated verification project required (pangolin-release-*, pangolin-realtime-*, pangolin-m6-*, or pangolin-m7-*)")
    dockerfile = "deploy/docker/Dockerfile.m6-runner" if args.target == "release" else "deploy/docker/Dockerfile.m7-runner"
    purpose_value = "release-test-runner" if args.target == "release" else "realtime-test-runner"
    image = project + "-runner"
    build_cmd = [
        "docker", "build", "-f", str(contracts.ROOT / dockerfile),
        "--label", "dev.pangolin.source-commit=" + contracts.revision(),
        "--label", "dev.pangolin.purpose=" + purpose_value,
        "-t", image, str(contracts.ROOT),
    ]
    subprocess.run(build_cmd, check=True)
    runner_id = subprocess.check_output(["docker", "image", "inspect", image, "--format", "{{.Id}}"], text=True).strip()
    network = project + "_default"
    cache = project + "-linux-cache"
    purpose = "dev.pangolin.purpose=" + purpose_value
    subprocess.run(["docker", "volume", "create", "--label", "dev.pangolin.owner=pangolin-framework", "--label", purpose, cache], check=True)
    host_drills = os.getenv("PANGOLIN_M6_DRILLS_DIR")
    if args.target == "release" and host_drills:
        drills = Path(host_drills).resolve()
        if not drills.is_relative_to(contracts.ROOT) or not (drills / "release-drills.json").is_file():
            parser.error("host drills require a release-drills.json inside this repository tree")
        host_drills = "/source/" + drills.relative_to(contracts.ROOT).as_posix()
    if args.target == "realtime":
        if not host_drills:
            parser.error("realtime Linux gate requires host-run drills (PANGOLIN_M6_DRILLS_DIR) executed on this host")
        if not os.getenv("PANGOLIN_M7_DEMO_KEY"):
            parser.error("realtime Linux gate requires PANGOLIN_M7_DEMO_KEY for the official-client harness")
        drills = Path(host_drills).resolve()
        if not drills.is_relative_to(contracts.ROOT) or not (drills / "release-drills.json").is_file():
            parser.error("host drills require a release-drills.json inside this repository tree")
        host_drills = "/source/" + drills.relative_to(contracts.ROOT).as_posix()
    barrier_rel = f"{args.target}-linux-broker-barrier"
    with nats_fixture.broker_restart(project, contracts.ROOT / "artifacts" / barrier_rel):
        environment = {
            "PANGOLIN_EXPECTED_SOURCE": test_runner.source_hash(),
            "PANGOLIN_VERIFY_PROJECT": project,
            "PANGOLIN_VERIFY_SCOPE": args.target,
            "PANGOLIN_M1_ADMIN_DSN": "postgres://pangolin_bootstrap:local-bootstrap-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_M1_APP_DSN": "postgres://pangolin_app:local-app-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_M1_WORKER_DSN": "postgres://pangolin_worker:local-worker-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_M1_NATS_URL": "nats://nats:4222",
            "PANGOLIN_M1_ISSUER": os.getenv("PANGOLIN_M1_ISSUER", "http://127.0.0.1:18180"),
            "PANGOLIN_M1_TOKEN_URL": "http://test-idp:8080/token",
            "PANGOLIN_JWKS_URL": "http://test-idp:8080/jwks.json",
            "PANGOLIN_M3_BROKER_RESTART_DIR": "/source/artifacts/" + barrier_rel,
            "PANGOLIN_DATABASE_POSTGRES_DSN": "postgres://pangolin_app:local-app-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_DATABASE_MYSQL_DSN": "pangolin_database:local-database-only@tcp(mysql:3306)/pangolin_db_contracts",
            "CGO_ENABLED": "1",
            "GOTOOLCHAIN": "local",
        }
        if args.target == "release":
            environment["PANGOLIN_M6_RUNNER_IMAGE_ID"] = runner_id
            environment["PANGOLIN_M6_RUNNER_IMAGE"] = image
            if host_drills:
                environment["PANGOLIN_M6_DRILLS_DIR"] = host_drills
        else:
            environment["PANGOLIN_M7_RUNNER_IMAGE_ID"] = runner_id
            environment["PANGOLIN_M7_RUNNER_IMAGE"] = image
            environment["PANGOLIN_M6_DRILLS_DIR"] = host_drills
            environment["PANGOLIN_M7_DEMO_KEY"] = os.environ["PANGOLIN_M7_DEMO_KEY"]
            environment["npm_config_cache"] = "/cache/npm-cache"
        command = [
            "docker", "run", "--rm", "--name", project + "-linux", "--cpus", "2", "--memory", "2g", "--network", network,
            "--label", "dev.pangolin.owner=pangolin-framework", "--label", purpose,
            "-v", str(contracts.ROOT) + ":/source", "-v", str(contracts.ROOT / ".cache/go-mod") + ":/gomod", "-v", cache + ":/cache",
        ]
        for key, value in environment.items():
            command.extend(["-e", key + "=" + value])
        command.extend([image, "python3", "/source/scripts/linux_entry.py"])
        return subprocess.call(command)


if __name__ == "__main__":
    raise SystemExit(main())
