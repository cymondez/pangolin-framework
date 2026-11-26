"""M6 executable release drills (A72/REL-005).

Runs the clean rebuild, forward upgrade, backup/restore, load baseline and
security review drills against the pinned fixture services using the same
PANGOLIN_M1_* environment as the earlier suites, then writes
<output>/release-drills.json. The report schema (status, commit, source
bytes, platform, project, the five named drill entries with exact command
records and checks) is the one validated by scripts/release_manifest.py;
extra top-level fields document provenance. The script fails closed on
command errors, skipped or missing tests, missing tools or services, and
missing or contradictory evidence. It never starts or stops services.
"""
import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path
from urllib.parse import urlsplit

import m0
import release_manifest

ROOT = m0.ROOT
REQUIRED_ENV = ("PANGOLIN_M1_ADMIN_DSN", "PANGOLIN_M1_APP_DSN", "PANGOLIN_M1_WORKER_DSN", "PANGOLIN_M1_NATS_URL", "PANGOLIN_M1_ISSUER")
EVIDENCE_ENV = "PANGOLIN_M6_EVIDENCE_DIR"
EXPECTED_UPGRADE_VERSIONS = [3000001, 3000002, 3100001, 3100002, 3100003, 3100004, 3200001, 3200002, 3300001, 4000001, 4000002, 4100002, 4100003]


def source_hash():
    """Same working-tree source identity as scripts/m1.py evidence."""
    paths = m0.command(["git", "-c", "core.quotepath=false", "ls-files", "--cached", "--others", "--exclude-standard"]).splitlines()
    digest = hashlib.sha256()
    for name in sorted(set(paths)):
        path = ROOT / name
        if path.is_file():
            digest.update(name.encode() + b"\0" + path.read_bytes())
    return digest.hexdigest()


def validate_go_tests(text, required):
    events = [json.loads(line) for line in text.splitlines() if line.startswith("{")]
    if not events or any(e.get("Action") in ("fail", "skip") and e.get("Test") for e in events):
        raise ValueError("drill test failed, was skipped or produced no JSON events")
    passed = {e.get("Package", "") + ":" + e.get("Test", "") for e in events if e.get("Action") == "pass"}
    missing = set(required) - passed
    if missing:
        raise ValueError("required drill test missing or not passed: " + ", ".join(sorted(missing)))


def endpoint(raw):
    """Non-secret endpoint summary of a fixture URL/DSN (no credentials)."""
    parsed = urlsplit(raw)
    if parsed.scheme in ("postgres", "postgresql"):
        return parsed.scheme + "://" + (parsed.hostname or "") + (":" + str(parsed.port) if parsed.port else "") + (parsed.path or "")
    return raw


def drill_env(output, extra=None):
    env = m0.go_environment(ROOT)
    env[EVIDENCE_ENV] = str(output)
    for key, value in (extra or {}).items():
        env[key] = value
    return env


class Drills:
    def __init__(self, args):
        self.output = Path(args.output).resolve()
        if (self.output / "release-drills.json").is_file():
            raise ValueError("refusing to overwrite an existing release-drills.json: " + str(self.output))
        self.output.mkdir(parents=True, exist_ok=True)
        if not re.fullmatch(r"pangolin-m6-[a-z0-9-]+", args.project):
            raise ValueError("--project must be a dedicated pangolin-m6-* project: " + str(args.project))
        self.project = args.project
        self.compose_project = args.compose_project or os.getenv("PANGOLIN_M6_COMPOSE_PROJECT") or args.project.removesuffix("-drills")
        if not re.fullmatch(r"pangolin-m6-[a-z0-9-]+", self.compose_project):
            raise ValueError("compose project must be a dedicated pangolin-m6-* project: " + self.compose_project)
        self.compose_files = args.compose_file or [os.getenv("PANGOLIN_M6_COMPOSE_FILE") or "deploy/compose/m1.yaml"]
        for name in self.compose_files:
            if not (ROOT / name).is_file():
                raise ValueError("compose file missing: " + name)
        self.pg_bin = args.pg_bin or os.getenv("PANGOLIN_PG_BIN") or os.getenv("PANGOLIN_M6_PG_BIN_DIR") or ""
        self.commit = m0.revision()
        self.source = source_hash()
        self.dirty = not m0.clean()
        platform = json.loads(m0.command(["go", "env", "-json", "GOOS", "GOARCH", "GOVERSION", "CGO_ENABLED"]))
        self.platform = platform["GOOS"] + "-" + platform["GOARCH"]
        self.platform_details = platform
        self.report = {
            "interface": "python scripts/m6_drills.py --output <dir> --project <project>",
            "baseline": "v1.3",
            "product_version": (ROOT / "VERSION").read_text(encoding="utf-8-sig").strip(),
            "commit": self.commit,
            "source_sha256": self.source,
            "working_tree_dirty": self.dirty,
            "evidence_class": "development-only" if self.dirty else "evidence-ready",
            "platform": self.platform,
            "platform_details": platform,
            "project": self.project,
            "compose": {"project": self.compose_project, "files": list(self.compose_files)},
            "inputs": {name: m0.digest(ROOT / name) for name in ("VERSION", "go.mod", "go.sum", "deploy/toolchain.json", "deploy/dependencies.json", "docs/security/release-review.md")},
            "environment": {},
            "services": {},
            "drills": {name: {"status": "failed", "error": None, "commands": [], "checks": {}} for name in release_manifest.DRILLS},
            "diagnostics": [],
            "status": "failed",
            "error": None,
        }
        missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
        if missing:
            raise ValueError("required fixture environment missing: " + ", ".join(missing))
        self.report["environment"] = {name: {"set": True, "endpoint": endpoint(os.getenv(name))} for name in REQUIRED_ENV}

    def execute(self, argv, name, drill, cwd=ROOT, env=None, check=True):
        started = time.monotonic()
        result = subprocess.run(argv, cwd=str(cwd), env=env or m0.go_environment(ROOT), capture_output=True, text=True, encoding="utf-8", errors="replace")
        log = self.output / (f"{len(self.report['drills'][drill]['commands']):02d}-{name}.log")
        log.write_text(result.stdout + result.stderr, encoding="utf-8")
        record = {"argv": [str(part) for part in argv], "exit_code": result.returncode, "log": log.name, "sha256": m0.digest(log)}
        self.report["drills"][drill]["commands"].append(record)
        self.report["diagnostics"].append({"drill": drill, "name": name, "seconds": round(time.monotonic() - started, 3)})
        if check and result.returncode:
            raise ValueError("drill command failed: " + name + "; see " + log.name)
        return result

    def evidence(self, name):
        return m0.load(self.output / name)

    def load(self, drill, test, evidence_file):
        """Run one drill Go test and require its exact pass set + evidence."""
        result = self.execute(["go", "test", "-count=1", "-json", "-tags=integration", "-run", "^" + test + "$", "./tests/m6"], "drill-" + drill, drill, env=drill_env(self.output))
        validate_go_tests(result.stdout, {m0.MODULE + "/tests/m6:" + test})
        evidence = self.evidence(evidence_file)
        if not isinstance(evidence, dict) or not evidence:
            raise ValueError("drill evidence missing: " + evidence_file)
        return evidence

    def finish(self, drill, checks, extra=None):
        entry = self.report["drills"][drill]
        entry["status"] = "passed"
        entry["checks"] = checks
        if extra:
            entry.update(extra)


def export_source(drills):
    """Copy the current working-tree source bytes to <output>/clean-source."""
    clean = drills.output / "clean-source"
    names = m0.command(["git", "-c", "core.quotepath=false", "ls-files", "--cached", "--others", "--exclude-standard"]).splitlines()
    copied = 0
    for name in sorted(set(names)):
        path = ROOT / name
        if not path.is_file() or drills.output == path or drills.output in path.parents:
            continue
        target = clean / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)
        copied += 1
    if copied == 0:
        raise ValueError("clean source export produced no files")
    return clean, copied


def clean_rebuild(drills):
    clean, files = export_source(drills)
    cache = drills.output / "clean-build"
    for name in ("go-cache", "go-tmp"):
        (cache / name).mkdir(parents=True, exist_ok=True)
    env = drill_env(drills.output, {"GOCACHE": str(cache / "go-cache"), "GOTMPDIR": str(cache / "go-tmp")})
    drills.execute(["go", "build", "./..."], "clean-source-build", "clean-rebuild", cwd=clean, env=env)
    result = drills.execute(["go", "test", "-count=1", "-json", "-tags=integration", "-run", "^TestM6CleanRebuild$", "./tests/m6"], "clean-rebuild-test", "clean-rebuild", cwd=clean, env=env)
    validate_go_tests(result.stdout, {m0.MODULE + "/tests/m6:TestM6CleanRebuild"})
    evidence = drills.evidence("clean-rebuild.json")
    post, reconciled, denials, http = evidence["post_commit"], evidence["worker_reconciled"], evidence["denials"], evidence["http"]
    if post["books_rows"] != 1 or post["outbox_rows"] != 1 or post["outbox_state"] != "pending":
        raise ValueError("clean rebuild 2xx did not prove a committed transaction: " + json.dumps(post))
    if reconciled["book_views_rows"] != 1 or reconciled["inbox_processed"] != 1 or reconciled["outbox_state"] != "sent" or reconciled["outbox_attempts"] != 1:
        raise ValueError("clean rebuild worker did not reconcile delivery: " + json.dumps(reconciled))
    if denials != {"forged_token_401": True, "tenant_header_forgery_403": True}:
        raise ValueError("clean rebuild entry denials missing: " + json.dumps(denials))
    if http["create_status"] != 201 or http["read_status"] != 200 or evidence["api"]["exit_code"] != 0 or evidence["worker"]["exit_code"] != 0:
        raise ValueError("clean rebuild HTTP/process evidence failed: " + json.dumps(evidence["http"]))
    if not evidence["database"].startswith("m6_rebuild_") or not evidence["build"]["api_sha256"] or not evidence["build"]["worker_sha256"]:
        raise ValueError("clean rebuild identity evidence incomplete")
    drills.report["services"]["postgres_server_version"] = evidence["server_version"]
    drills.report["services"]["nats_server_version"] = evidence["nats_server_version"]
    drills.report["services"]["issuer"] = os.getenv("PANGOLIN_M1_ISSUER")
    shutil.rmtree(clean)
    shutil.rmtree(cache)
    drills.finish("clean-rebuild", {
        "clean_source": True, "clean_database": True, "clean_build": True, "tests_passed": True,
    }, extra={"exported_files": files, "build": evidence["build"], "database": evidence["database"]})


def forward_upgrade(drills):
    evidence = drills.load("forward-upgrade", "TestM6ForwardUpgrade", "upgrade-drill.json")
    upgraded, previous, probes = evidence["upgraded_deployment"], evidence["previous_deployment"], evidence["post_upgrade_probes"]
    if previous["policies_on_books"] != ["tenant_books"] or previous["applied_versions"] != []:
        raise ValueError("forward upgrade did not start from the previous tenant-only deployment")
    if evidence["upgrade"]["versions_applied"] != EXPECTED_UPGRADE_VERSIONS or evidence["upgrade"]["versions_applied"] != evidence["upgrade"]["expected_versions"]:
        raise ValueError("forward upgrade applied unexpected migration versions")
    if not upgraded["books_table_gone"] or not upgraded["projection_match"] or upgraded["owner_violations"] != 0:
        raise ValueError("forward upgrade lost or mutated previous data: " + json.dumps(upgraded))
    if upgraded["policies_on_scope_books"] != ["exact_owner"] or upgraded["permissive_policy_union"] or not upgraded["force_rls_on_scope_books"][0] or not upgraded["force_rls_on_scope_books"][1]:
        raise ValueError("forward upgrade left a permissive or unforced policy state: " + json.dumps(upgraded))
    if upgraded["resource_scopes"] != ["tenant-a:tenant:tenant-a", "tenant-b:tenant:tenant-b"]:
        raise ValueError("forward upgrade guessed owner scopes: " + json.dumps(upgraded["resource_scopes"]))
    if probes["cross_tenant_rows_visible"] != 0 or not probes["old_api_probe_failed"] or not probes["org_owner_insert_ok"] or not probes["tenant_scope_cannot_read_org_rows"]:
        raise ValueError("forward upgrade post-probes failed: " + json.dumps(probes))
    seeded = upgraded["owner_preserved_rows"]
    if seeded < 1:
        raise ValueError("forward upgrade recorded no seeded rows")
    drills.finish("forward-upgrade", {
        "seeded_tenant_rows": seeded, "owner_preserved": True, "force_rls": True, "permissive_policy_left": False,
    }, extra={"database": evidence["database"], "upgrade_seconds": evidence["upgrade"]["seconds"], "compatibility": evidence["upgrade"]["compatibility"]})


def resolve_pg(drills):
    """Fail-closed pg client resolution; container mode shells into the
    already-running fixture postgres of the drills' compose project."""
    hints = {"PANGOLIN_M6_PG_MODE": ""}
    if os.getenv("PANGOLIN_M6_PG_MODE", "") not in ("", "host", "container"):
        raise ValueError("unknown M6 PostgreSQL client mode")
    if drills.pg_bin:
        for program in ("pg_dump", "pg_restore"):
            suffix = ".exe" if os.name == "nt" else ""
            if not (Path(drills.pg_bin) / (program + suffix)).is_file():
                raise ValueError("pg client binary missing in " + drills.pg_bin + ": " + program)
        hints.update({"PANGOLIN_M6_PG_MODE": "host", "PANGOLIN_M6_PG_BIN_DIR": drills.pg_bin})
    elif os.getenv("PANGOLIN_M6_PG_MODE") != "container" and shutil.which("pg_dump") and shutil.which("pg_restore"):
        hints.update({"PANGOLIN_M6_PG_MODE": "host", "PANGOLIN_M6_PG_BIN_DIR": ""})
    else:
        try:
            arguments = ["docker", "compose", "-p", drills.compose_project]
            for name in drills.compose_files:
                arguments += ["-f", name]
            probe = subprocess.run(arguments + ["ps", "--format", "json"], cwd=str(ROOT), capture_output=True, text=True, encoding="utf-8", errors="replace")
        except OSError as error:
            raise ValueError("host pg clients unavailable and Docker is not runnable (" + str(error) + "); install pg_dump/pg_restore >= server major, or run the host-run procedure in docs/runbooks/release.md")
        log = drills.output / "pg-docker-probe.log"
        log.write_text(probe.stdout + probe.stderr, encoding="utf-8")
        if probe.returncode:
            raise ValueError("host pg clients unavailable and the Docker fixture is not reachable; install pg_dump/pg_restore >= server major, or run the host-run procedure in docs/runbooks/release.md")
        try:
            raw = probe.stdout.strip()
            containers = json.loads(raw) if raw.startswith("[") else [json.loads(line) for line in raw.splitlines() if line.strip()]
        except json.JSONDecodeError:
            raise ValueError("cannot inspect the Docker fixture; run the host-run procedure in docs/runbooks/release.md")
        if not any(entry.get("Service") == "postgres" and entry.get("State") == "running" for entry in containers):
            raise ValueError("the compose project " + drills.compose_project + " has no running postgres service; pass --compose-project of the inventoried fixture project (see docs/runbooks/release.md)")
        hints.update({"PANGOLIN_M6_PG_MODE": "container", "PANGOLIN_M6_PG_CONTAINER_ARGV": json.dumps(arguments + ["exec", "-T", "postgres"])})
    return hints


def backup_restore(drills):
    hints = resolve_pg(drills)
    env = drill_env(drills.output, hints)
    result = drills.execute(["go", "test", "-count=1", "-json", "-tags=integration", "-run", "^TestM6BackupRestore$", "./tests/m6"], "drill-backup-restore", "backup-restore", env=env)
    validate_go_tests(result.stdout, {m0.MODULE + "/tests/m6:TestM6BackupRestore"})
    evidence = drills.evidence("backup-restore.json")
    pg, restore, rpo, rto = evidence["pg"], evidence["restore"], evidence["rpo"], evidence["rto"]
    if pg["mode"] not in ("host", "container") or pg["dump_exit"] != 0 or pg["restore_exit"] != 0:
        raise ValueError("backup/restore pg execution failed: " + json.dumps(pg))
    if not (drills.output / "backup.dump").is_file() or m0.digest(drills.output / "backup.dump") != pg["dump_sha256"]:
        raise ValueError("backup dump missing or its hash does not match the recorded archive")
    if restore["mismatches"] or restore["tables_compared"] < 1:
        raise ValueError("restored database did not match the source: " + json.dumps(restore["mismatches"]))
    if not restore["rls_flags_match"] or not restore["policies_match"] or not restore["roles_match"] or restore["app_role_tenant_a_rows"] != 25:
        raise ValueError("restored control state incomplete: " + json.dumps(restore))
    if rpo["rows_missing"] != 0 or rpo["window_seconds"] < 0 or rto["total_seconds"] <= 0:
        raise ValueError("backup/restore RPO/RTO evidence invalid: " + json.dumps({"rpo": rpo, "rto": rto}))
    if not evidence["database"].startswith("m6_backup_") or not evidence["restore_database"].startswith("m6_restore_"):
        raise ValueError("backup/restore did not use dedicated drill databases")
    drills.finish("backup-restore", {
        "restore_verified": True, "row_counts_match": True, "checksums_match": True, "independent_connection": True,
    }, extra={"database": evidence["database"], "restore_database": evidence["restore_database"], "pg": pg, "rpo": rpo, "rto": rto, "source_tables": len(evidence["source"]["tables"])})


def load_baseline(drills):
    evidence = drills.load("load-baseline", "TestM6LoadBaseline", "load-baseline.json")
    load, percentiles, samples = evidence["load"], evidence["percentiles"], evidence["samples_us"]
    if len(samples) != load["ops"] or len(evidence["warmup_samples_us"]) != load["warmup_ops"]:
        raise ValueError("load baseline lost samples: " + str(len(samples)) + " of " + str(load["ops"]))
    if evidence["reconciled"]["errors"] != 0 or evidence["reconciled"]["books_rows"] != load["ops"] + load["warmup_ops"] or evidence["reconciled"]["outbox_rows"] != load["ops"] + load["warmup_ops"]:
        raise ValueError("load baseline reconciliation failed: " + json.dumps(evidence["reconciled"]))
    ordered = sorted(samples)
    for rank, key in ((50, "p50_us"), (95, "p95_us"), (99, "p99_us")):
        if percentiles[key] != ordered[(len(ordered) * rank) // 100]:
            raise ValueError("load baseline percentile mismatch: " + key)
    if percentiles["p95_us"] <= 0 or percentiles["p99_us"] <= 0 or evidence["ops_per_second"] <= 0:
        raise ValueError("load baseline recorded non-positive measurements")
    hardware = evidence["hardware"]
    drills.finish("load-baseline", {
        "hardware": hardware["goos"] + "/" + hardware["goarch"] + ", " + str(hardware["num_cpu"]) + " CPUs, GOMAXPROCS " + str(hardware["gomaxprocs"]),
        "go_version": hardware["go_version"],
        "database": "PostgreSQL " + evidence["server_version"] + ", pool max " + str(load_pool(evidence)) + ", role pangolin_app",
        "broker": "NATS " + drills.report.get("services", {}).get("nats_server_version", m0.load(ROOT / "deploy/toolchain.json")["nats"]) + " fixture (not exercised by this drill)",
        "methodology": evidence["method"],
        "pool_size": load_pool(evidence),
        "load_count": load["ops"],
        "concurrency": load["concurrency"],
        "p95_ms": percentiles["p95_us"] / 1000.0,
        "p99_ms": percentiles["p99_us"] / 1000.0,
        "samples": [value / 1000.0 for value in samples],
    }, extra={"database": evidence["database"], "duration_seconds": evidence["duration_seconds"], "ops_per_second": evidence["ops_per_second"], "heap": evidence["heap"]})


def load_pool(evidence):
    return int(evidence["database_pool"]["max_conns"])


def security_controls():
    """Parse the machine-readable control block from the security review."""
    text = (ROOT / "docs/security/release-review.md").read_text(encoding="utf-8-sig")
    for block in re.findall(r"```json\n(.*?)```", text, re.DOTALL):
        data = json.loads(block)
        if isinstance(data, list) and data and all(isinstance(item, dict) and "category" in item for item in data):
            if {item["category"] for item in data} == set(release_manifest.THREAT_CATEGORIES):
                for item in data:
                    if set(item) != {"category", "attack_surface", "abuse_path", "control", "test", "residual_risk"} or any(not str(item[key]).strip() for key in item):
                        raise ValueError("invalid security control record for " + item["category"])
                return data, text
    raise ValueError("docs/security/release-review.md has no control block covering every specification 14.1 threat category")


def security_review(drills):
    controls, text = security_controls()
    unresolved = [control["category"] for control in controls if re.search(r"(?i)unresolved.*high|high.*unresolved", control["residual_risk"])]
    if unresolved:
        raise ValueError("unresolved high-risk security findings block the release: " + ", ".join(unresolved))
    if any(re.search(r"(?i)remaining risk:\s*high", line) for line in text.splitlines()):
        raise ValueError("security review records a high remaining risk")
    by_package = {}
    for control in controls:
        for selector in [part.strip() for part in control["test"].split(";") if part.strip()]:
            package, _, test = selector.rpartition(":")
            if not package or not test:
                raise ValueError("invalid security control test selector: " + selector)
            by_package.setdefault(package, set()).add("^" + test + "$")
    results = []
    for package in sorted(by_package):
        arguments = ["go", "test", "-count=1", "-json"]
        if package.startswith("tests/"):
            arguments.append("-tags=integration")
        arguments += ["-run", "|".join(sorted(by_package[package])), "./" + package.removeprefix("./")]
        result = drills.execute(arguments, "security-" + package.split("/")[-1], "security-review", env=drill_env(drills.output))
        required = {m0.MODULE + "/" + package.removeprefix("./") + ":" + test[1:-1] for test in by_package[package]}
        validate_go_tests(result.stdout, required)
        results.extend(sorted(required))
    drill = drills.evidence("security-drill.json")
    if drill["rls_violations"] or drill["unowned_tables"] != 0 or not drill["injection"]["stored_exactly"]:
        raise ValueError("security drill database controls failed: " + json.dumps(drill))
    idor = drill["idor"]
    if idor["cross_tenant_filterless"] != 0 or idor["cross_tenant_write"] != 0 or idor["cross_tenant_soft_delete"] != 0 or idor["tenant_sees_org_rows"] != 0 or idor["other_tenant_org_rows"] != 0:
        raise ValueError("security drill IDOR probes failed: " + json.dumps(idor))
    for role, attributes in drill["roles"].items():
        if attributes["superuser"] or attributes["bypassrls"]:
            raise ValueError("security drill role posture failed: " + role)
    (drills.output / "security-tests.json").write_text(json.dumps({"passed": sorted(results), "controls": controls}, indent=2) + "\n", encoding="utf-8")
    drills.finish("security-review", {
        "categories": list(release_manifest.THREAT_CATEGORIES),
        "unresolved_high_risk": len(unresolved),
        "controls": controls,
    }, extra={"executable_tests": sorted(results), "review_document": "docs/security/release-review.md", "database": drill["database"]})


def run(drills):
    steps = {"clean-rebuild": clean_rebuild, "forward-upgrade": forward_upgrade, "backup-restore": backup_restore, "load-baseline": load_baseline, "security-review": security_review}
    for name in release_manifest.DRILLS:
        try:
            steps[name](drills)
        except Exception as error:
            drills.report["drills"][name]["error"] = str(error)
            drills.report["error"] = name + ": " + str(error)
    passed = all(drills.report["drills"][name]["status"] == "passed" for name in release_manifest.DRILLS)
    if source_hash() != drills.source or m0.revision() != drills.commit:
        passed = False
        drills.report["error"] = "source changed during release drills"
    drills.report["status"] = "passed" if passed else "failed"
    drills.report["artifacts"] = {}
    for path in sorted(drills.output.rglob("*")):
        if path.is_file() and path.name != "release-drills.json":
            drills.report["artifacts"][path.relative_to(drills.output).as_posix()] = m0.digest(path)
    (drills.output / "release-drills.json").write_text(json.dumps(drills.report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"status": drills.report["status"], "error": drills.report["error"], "report": str(drills.output / "release-drills.json")}, indent=2))
    return 0 if passed else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True)
    parser.add_argument("--project", required=True)
    parser.add_argument("--pg-bin", default="")
    parser.add_argument("--compose-file", action="append")
    parser.add_argument("--compose-project", default="")
    args = parser.parse_args()
    try:
        return run(Drills(args))
    except (ValueError, OSError, KeyError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        print("M6 drills failed:", error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
