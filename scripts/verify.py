"""Unified cumulative verification and two-platform release Gate CLI.

Usage:
  python scripts/verify.py verify --project <name> [--race] [--scope release|realtime]
  python scripts/verify.py gate --commit <40-hex-sha> [--scope release|realtime]

Scopes:
  - release (default): full v0.1.0 cumulative acceptance (95 requirements / 532
    named subtests, real PostgreSQL/MySQL/SQLite/NATS/IdP suites, multi-process
    E2E, CLI-generated Bookstore profiles, five executable release drills, bound
    release manifest, >=90% raw statement coverage, and Spec 13.6 evidence index).
  - realtime: full cumulative release acceptance plus the additive SignalR
    realtime acceptance (104 requirements / 593 named subtests, official
    @microsoft/signalr 10.0.0 client harness across three release profiles,
    npm dependency/license lock, and >=90% raw coverage including
    modules/realtime/... and events/nats).
"""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
from urllib.parse import urlsplit, urlunsplit
import uuid

import client_dependencies
import contracts
import evidence_index
import nats_fixture
import release_manifest
import test_runner

ROOT = contracts.ROOT
PROJECT_RE = r"pangolin-(?:m[67]|release|realtime)-[a-z0-9-]+"
M5_FIELDS = ("unit_tests", "integration_tests", "python_tests", "template_tests")
M6_FIELDS = ("unit_tests", "integration_tests", "python_tests", "drills")
M7_FIELDS = (*evidence_index.FIELDS, "gate_checks")
REALTIME_MANIFEST_REL = "deploy/release/m7.json"
REALTIME_DOCKERFILE_REL = "deploy/docker/Dockerfile.m7-runner"
REALTIME_MAPPING_REL = "tests/contract/m7.json"
GATE_CONTRACT = evidence_index.GATE_CONTRACT
REALTIME_DATABASE_FORMAT = re.compile(r"m5_template_m7rt_[0-9a-f]{12}")


def database_env(env=None):
    """Resolve both real backend DSNs; explicit values win over the host fixture."""
    values = dict(os.environ if env is None else env)
    postgres = (values.get("PANGOLIN_DATABASE_POSTGRES_DSN") or "").strip()
    if not postgres:
        postgres = values.get("PANGOLIN_M1_APP_DSN") or "postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable"
    port = (values.get("PANGOLIN_M5_MYSQL_PORT") or "").strip() or "15543"
    int(port)
    mysql = (values.get("PANGOLIN_DATABASE_MYSQL_DSN") or "").strip() or "pangolin_database:local-database-only@tcp(127.0.0.1:" + port + ")/pangolin_db_contracts"
    if not postgres.strip() or not mysql.strip():
        raise ValueError("database acceptance requires non-empty PANGOLIN_DATABASE_POSTGRES_DSN and PANGOLIN_DATABASE_MYSQL_DSN")
    return {"PANGOLIN_DATABASE_POSTGRES_DSN": postgres, "PANGOLIN_DATABASE_MYSQL_DSN": mysql}


def prepare_pg_clients(project):
    """Select PostgreSQL clients before cumulative integration and race."""
    mode = os.getenv("PANGOLIN_M6_PG_MODE", "")
    if mode:
        if mode not in ("host", "container"):
            raise ValueError("unknown PostgreSQL client mode")
        if mode == "container" and not os.getenv("PANGOLIN_M6_PG_CONTAINER_ARGV"):
            raise ValueError("container PostgreSQL clients require a command prefix")
        return
    major = int(contracts.load(ROOT / "deploy/toolchain.json")["postgres"].split(".")[0])
    native = True
    for program in ("pg_dump", "pg_restore"):
        executable = shutil.which(program)
        if not executable:
            native = False
            break
        version = subprocess.run([executable, "--version"], capture_output=True, text=True, check=False)
        match = re.search(r"PostgreSQL\)\s+(\d+)", version.stdout)
        if version.returncode or not match or int(match[1]) < major:
            native = False
            break
    if native:
        os.environ["PANGOLIN_M6_PG_MODE"] = "host"
    else:
        if not re.fullmatch(PROJECT_RE, project):
            raise ValueError("PostgreSQL client fallback requires an isolated verification project")
        os.environ["PANGOLIN_M6_PG_MODE"] = "container"
        os.environ["PANGOLIN_M6_PG_CONTAINER_ARGV"] = json.dumps([
            "docker", "compose", "-p", project, "-f", str(ROOT / "deploy/compose/m1.yaml"),
            "exec", "-T", "postgres",
        ])


def validate_mapping(mapping, milestone=None):
    """Fail closed unless every named subtest for the milestone is mapped with nothing pending."""
    target = milestone or mapping.get("milestone")
    if target not in ("M2", "M3", "M4", "M5", "M6", "M7"):
        raise ValueError("unsupported milestone mapping: " + str(target))
    groups = contracts.load(ROOT / "tests/contract/subtests.json")["groups"]
    expected = {subtest["id"] for group in groups if group["milestone"] == target for subtest in group["subtests"]}
    fields = {
        "M2": ("unit_tests", "integration_tests", "python_tests"),
        "M3": ("unit_tests", "integration_tests", "python_tests"),
        "M4": ("unit_tests", "integration_tests", "python_tests"),
        "M5": M5_FIELDS,
        "M6": M6_FIELDS,
        "M7": M7_FIELDS,
    }[target]
    sets = [set(mapping.get(key) or {}) for key in fields]
    if mapping.get("milestone") != target or mapping.get("baseline") != "v1.3" or set.union(*sets) != expected or sum(map(len, sets)) != len(expected):
        raise ValueError(target + " acceptance mapping incomplete or duplicated")
    if target != "M2" and mapping.get("pending"):
        details = ", ".join(entry.get("id", "?") if isinstance(entry, dict) else str(entry) for entry in mapping["pending"])
        raise ValueError(target + " has unimplemented acceptance: " + details)
    if any(not identity for key in fields for identity in (mapping.get(key) or {}).values()):
        raise ValueError(target + " has empty acceptance identities")
    if target in ("M5", "M6"):
        field = mapping.get("regression_integration_tests", {})
        if any(not name for name in (field.values() if isinstance(field, dict) else field)):
            raise ValueError(target + " regression integration mapping incomplete")
    if target == "M6":
        drill_ids = {subtest["id"] for group in groups if group["milestone"] == "M6" for subtest in group["subtests"] if subtest["id"].startswith("A72/")}
        if set(mapping["drills"]) != drill_ids or sorted(mapping["drills"].values()) != sorted(release_manifest.DRILLS):
            raise ValueError("M6 drills must map every A72 subtest to its executable drill")
    if target == "M7":
        for subtest, identity in (mapping.get("gate_checks") or {}).items():
            if identity != evidence_index.GATE_CONTRACT:
                raise ValueError("M7 gate_checks identity must bind " + evidence_index.GATE_CONTRACT + ": " + subtest)
        for field in ("unit_tests", "integration_tests"):
            for subtest, identity in (mapping.get(field) or {}).items():
                if not re.fullmatch(contracts.MODULE.replace(".", r"\.") + r"/[A-Za-z0-9_./-]+:Test[A-Za-z0-9_/.-]+", identity):
                    raise ValueError("M7 " + field + " identity is not a bound Go test: " + subtest + " -> " + identity)
        for subtest, identity in (mapping.get("python_tests") or {}).items():
            if not re.fullmatch(r"[A-Za-z0-9_.]+", identity):
                raise ValueError("M7 python_tests identity is not an unittest name: " + subtest)
        for subtest, identity in (mapping.get("template_tests") or {}).items():
            if identity not in (evidence_index.REALTIME_CONTRACT, evidence_index.TEMPLATE_CONTRACT):
                raise ValueError("M7 template identity is not a supported contract: " + subtest + " -> " + identity)
        for field in ("regression_tests", "regression_integration_tests"):
            field_value = mapping.get(field)
            names = field_value.values() if isinstance(field_value, dict) else (field_value or [])
            if any(not isinstance(name, str) or not name for name in names):
                raise ValueError("M7 regression mapping incomplete")
    return mapping


def drills_project(project):
    """Drills reuse the running verification service project for docker compose pg fallback."""
    if not re.fullmatch(PROJECT_RE, project):
        raise ValueError("release drills require a dedicated verification service project: " + project)
    return project


def adopt_host_drills(report, source, target):
    """Check a host-run drill report's provenance, then copy it byte-exact."""
    source = Path(source).resolve()
    target = Path(target)
    if source == target.resolve() or not source.is_relative_to((ROOT / "artifacts").resolve()):
        raise ValueError("host drills must come from a distinct directory inside workspace artifacts")
    report_path = source / "release-drills.json"
    if not report_path.is_file():
        raise ValueError("host drill report missing: " + str(report_path))
    data = contracts.load(report_path)
    platform = report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"]
    release_manifest.validate_drills(data, source, report["commit"], report["source_sha256"], platform, project=data.get("project"), host_run=True)
    target.mkdir(parents=True, exist_ok=True)
    for path in sorted(path for path in source.rglob("*") if path.is_file()):
        destination = target / path.relative_to(source)
        if not destination.resolve().is_relative_to(target.resolve()):
            raise ValueError("drill artifact escapes its directory: " + str(path))
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, destination)
    copied = contracts.load(target / "release-drills.json")
    release_manifest.validate_drills(copied, target, report["commit"], report["source_sha256"], platform, project=copied.get("project"), host_run=True)
    return {"host_run": True, "project": copied["project"]}


def release_steps(report, output, execute):
    """Injected into the cumulative gate after coverage: manifest, drills, evidence."""
    execute([sys.executable, "scripts/release_manifest.py", "--check"], "release-manifest")
    output = Path(output)
    drills = output / "release-drills"
    host = os.getenv("PANGOLIN_M6_DRILLS_DIR")
    if host:
        report["release_drills"] = adopt_host_drills(report, host, drills)
    else:
        project = drills_project(report["project"])
        execute([sys.executable, "scripts/release_drills.py", "--output", str(drills), "--project", project], "release-drills")
        report["release_drills"] = {"host_run": False, "project": project}
    report["inputs"]["deploy/release/manifest.json"] = contracts.digest(ROOT / "deploy/release/manifest.json")
    if os.getenv("PANGOLIN_M6_RUNNER_IMAGE_ID"):
        report["runner_image"] = {
            "name": os.environ["PANGOLIN_M6_RUNNER_IMAGE"],
            "id": os.environ["PANGOLIN_M6_RUNNER_IMAGE_ID"],
            "dockerfile_sha256": contracts.digest(ROOT / "deploy/docker/Dockerfile.m6-runner"),
        }
    evidence = release_manifest.build_evidence(report, output)
    index = evidence_index.write_index({**report, "status": "passed"}, output, scope="release")
    report["profile_evidence"] = {"path": evidence_index.EVIDENCE_FILE, "totals": index["totals"]}
    report["release"] = {
        "status": evidence["status"],
        "product_version": evidence["product_version"],
        "drills": [entry["name"] for entry in evidence["drills"]["entries"]],
        "critical_packages": [entry["package"] for entry in evidence["coverage"]["critical"]],
        "drills_provenance": evidence["drills"]["provenance"],
    }


def _realtime_env_names():
    return ("PANGOLIN_M1_ADMIN_DSN", "PANGOLIN_M1_APP_DSN", "PANGOLIN_M1_WORKER_DSN", "PANGOLIN_M1_NATS_URL", "PANGOLIN_M1_ISSUER", "PANGOLIN_M7_DEMO_KEY")


def replace_database(dsn, database):
    parts = urlsplit(dsn)
    return urlunsplit(parts._replace(path="/" + database))


def prepare_realtime(report, output, execute, packages):
    """Same-source realtime fixture, recorded before the unit suite runs."""
    output = Path(output)
    missing = [name for name in _realtime_env_names() if not os.getenv(name)]
    if missing:
        raise ValueError("realtime preparation environment missing: " + ", ".join(missing))
    covered = output / "realtime-cov"
    covered.mkdir(exist_ok=True)
    platform = report["platform"]
    race = bool(report.get("requested_race")) and platform["GOOS"] == "linux"
    node = shutil.which("node")
    npm = shutil.which("npm.cmd" if os.name == "nt" else "npm")
    if not node or not npm:
        raise ValueError("official-client harness requires Node.js and npm on the runner")
    execute(["node", "--version"], "node-version")
    execute([npm, "--version"], "npm-version")
    execute([npm, "--prefix", "examples/realtime/client", "ci", "--ignore-scripts"], "realtime-client-deps")
    db_tool = output / ("realtime-db.exe" if os.name == "nt" else "realtime-db")
    execute(["go", "build", "-o", str(db_tool), "./internal/testkit/cmd/template-db"], "realtime-dbtool")
    database = "m5_template_m7rt_" + uuid.uuid4().hex[:12]
    execute([str(db_tool), "create", database], "realtime-db-create")
    binary = output / ("realtime-demo.exe" if os.name == "nt" else "realtime-demo")
    argv = ["go", "build", "-cover", "-covermode=atomic"]
    if race:
        argv.append("-race")
    argv += ["-coverpkg=" + packages, "-o", str(binary), "./examples/realtime"]
    execute(argv, "realtime-demo")
    os.environ.update({
        "GOCOVERDIR": str(covered),
        "PANGOLIN_REALTIME_BINARY": str(binary),
        "PANGOLIN_REALTIME_DEMO_KEY": os.environ["PANGOLIN_M7_DEMO_KEY"],
        "PANGOLIN_REALTIME_ADMIN_DSN": replace_database(os.environ["PANGOLIN_M1_ADMIN_DSN"], database),
        "PANGOLIN_REALTIME_APP_DSN": replace_database(os.environ["PANGOLIN_M1_APP_DSN"], database),
        "PANGOLIN_REALTIME_WORKER_DSN": replace_database(os.environ["PANGOLIN_M1_WORKER_DSN"], database),
        "PANGOLIN_REALTIME_NATS_URL": os.environ["PANGOLIN_M1_NATS_URL"],
    })
    execute(["go", "run", "./examples/realtime", "--provision"], "realtime-provision")
    execute([str(binary), "--cleanup-stream", "PANGOLIN_REALTIME_DEMO"], "realtime-broker-cleanup")
    return {
        "realtime": {
            "database": database,
            "database_tool": db_tool.name,
            "binary": binary.name,
            "provisioned": True,
            "race": race,
            "coverage_dir": covered.name,
            "covered_child_binary": True,
        }
    }


def realtime_coverage_steps(report, output, execute, packages):
    """Official-client E2E on the prepared fixture; returns the coverage profile."""
    output = Path(output)
    prepared = (report.get("prepare") or {}).get("realtime")
    if not isinstance(prepared, dict) or not prepared.get("provisioned"):
        raise ValueError("realtime fixture was not prepared before the acceptance suites")
    node = shutil.which("node")
    npm = shutil.which("npm.cmd" if os.name == "nt" else "npm")
    if not node or not npm:
        raise ValueError("official-client harness requires Node.js and npm on the runner")
    missing = [
        name for name in ("PANGOLIN_REALTIME_BINARY", "PANGOLIN_REALTIME_ADMIN_DSN", "PANGOLIN_REALTIME_APP_DSN", "PANGOLIN_REALTIME_WORKER_DSN", "PANGOLIN_REALTIME_NATS_URL", "PANGOLIN_REALTIME_DEMO_KEY")
        if not os.getenv(name)
    ]
    if missing:
        raise ValueError("realtime acceptance environment missing: " + ", ".join(missing))
    binary = Path(os.environ["PANGOLIN_REALTIME_BINARY"])
    if not binary.is_file():
        raise ValueError("prepared realtime binary is missing: " + str(binary))
    covered = output / prepared["coverage_dir"]
    execute([sys.executable, "scripts/client_dependencies.py", "--check", "--output", str(output / "client-dependencies.json")], "realtime-client-licenses")
    stream = execute([npm, "--prefix", "examples/realtime/client", "test", "--", "--test-reporter=tap"], "realtime-e2e")
    parsed = evidence_index.parse_official_log(stream)
    raw = {"kind": "official-client-harness", "status": "passed", "command": "realtime-e2e", "tests": parsed["tests"], "skipped": 0, "failed": 0, "profiles": parsed["profiles"]}
    (output / evidence_index.RELEASE_E2E_FILE).write_bytes((json.dumps(raw, indent=2) + "\n").encode("utf-8"))
    if not list(covered.glob("covmeta.*")) or not list(covered.glob("covcounters.*")):
        raise ValueError("official-client harness produced no coverage instrumentation")
    execute(["go", "tool", "covdata", "textfmt", "-i=" + str(covered), "-o=" + str(output / "e2e-realtime.out")], "realtime-coverage")
    db_tool = output / prepared["database_tool"]
    execute([str(db_tool), "drop", str(prepared["database"])], "realtime-db-drop")
    return [output / "e2e-realtime.out"]


def validate_realtime_manifest(manifest, root=ROOT):
    """Validate the additive realtime release manifest against every real source."""
    root = Path(root)
    keys = ("milestone", "baseline", "product_version", "version_scope", "requirements", "subtests", "profiles", "critical_packages", "drills", "gates", "version_bindings", "assets")
    unknown = sorted(set(manifest) - set(keys))
    missing = [key for key in keys if key not in manifest]
    if missing or unknown:
        raise ValueError("realtime release manifest keys missing or unknown: " + ", ".join(missing + unknown))
    if manifest["milestone"] != "M7" or manifest["baseline"] != "v1.3":
        raise ValueError("realtime release manifest baseline drift")
    if manifest["product_version"] != release_manifest.PRODUCT_VERSION or manifest["version_scope"] != release_manifest.VERSION_SCOPE:
        raise ValueError("realtime keeps VERSION at " + release_manifest.PRODUCT_VERSION + ": the realtime subsystem is not a tag release")
    if manifest["requirements"] != evidence_index.BASELINE_REQUIREMENTS + evidence_index.REALTIME_REQUIREMENTS or manifest["subtests"] != evidence_index.REALTIME_SUBTESTS:
        raise ValueError("realtime release manifest must bind " + str(evidence_index.BASELINE_REQUIREMENTS + evidence_index.REALTIME_REQUIREMENTS) + " requirements and " + str(evidence_index.REALTIME_SUBTESTS) + " named subtests")
    if [profile["profile"] for profile in manifest["profiles"]] != list(release_manifest.PROFILES):
        raise ValueError("realtime release manifest profiles must list all three applicability rows")
    for profile in manifest["profiles"]:
        if set(profile) != {"profile", "applicability_source"} or profile["applicability_source"] != f"tests/contract/profiles/{profile['profile']}.json":
            raise ValueError("realtime profile applicability source drift: " + profile.get("profile", "?"))
    required = set(release_manifest.load_manifest(root)["critical_packages"]) | {"modules/realtime", "events/nats"}
    if sorted(set(manifest["critical_packages"])) != sorted(required):
        raise ValueError("realtime critical packages must match the historical release list plus realtime and events/nats")
    if set(manifest["drills"]) != set(release_manifest.DRILLS) or manifest["gates"] != list(release_manifest.GATES):
        raise ValueError("realtime release manifest must reuse the five executable drills and both verified platforms")
    versions = release_manifest.check_versions(manifest, root)
    assets = manifest["assets"]
    if not assets or len(set(assets)) != len(assets) or assets[0] != REALTIME_MANIFEST_REL:
        raise ValueError("realtime release assets must be a non-empty unique list starting with the manifest itself")
    required_assets = (REALTIME_MANIFEST_REL, "deploy/release/m7-client-dependencies.json", "docs/guides/m7-development.md", REALTIME_DOCKERFILE_REL)
    if any(name not in assets for name in required_assets):
        raise ValueError("missing realtime release asset(s): " + ", ".join(name for name in required_assets if name not in assets))
    for name in assets:
        path = (Path(root) / name).resolve()
        if not path.is_relative_to(Path(root).resolve()) or not path.is_file() or path.stat().st_size == 0:
            raise ValueError("missing realtime release asset: " + name)
    return {
        "requirements": manifest["requirements"],
        "subtests": manifest["subtests"],
        "profiles": release_manifest.profile_rows(root),
        "versions": versions,
        "critical_packages": sorted(set(manifest["critical_packages"])),
        "assets": {name: contracts.digest(Path(root) / name) for name in assets},
    }


def validate_client_dependencies(output, root=ROOT):
    """Re-derive the raw client-dependencies artifact against the reviewed manifest."""
    root, output = Path(root), Path(output)
    manifest = contracts.load(root / "deploy/release/m7-client-dependencies.json")
    artifact = contracts.load(output / "client-dependencies.json")
    client_dependencies.validate(manifest, artifact)
    return artifact


def build_release_realtime(report, output, root=ROOT):
    """Derive the realtime release evidence purely from raw artifacts (no writes)."""
    root, output = Path(root), Path(output)
    summary = validate_realtime_manifest(contracts.load(root / REALTIME_MANIFEST_REL), root)
    groups = contracts.load(root / "tests/contract/subtests.json")["groups"]
    mappings = {number: contracts.load(root / f"tests/contract/m{number}.json") for number in range(1, 8)}
    mapping = mappings[max(mappings)]
    closure = release_manifest.check_closure(mappings, groups)
    if closure["mapped"] != evidence_index.REALTIME_SUBTESTS or closure["milestones"].get("M7") != evidence_index.REALTIME_NAMED:
        raise ValueError("realtime release closure is incomplete: " + str(closure["milestones"]))
    regressions = release_manifest.validate_regression_logs(report, output, extra={"m7-regression": set()})
    unit, integration = set(report["tests"]), set(report["integration_tests"])
    for field, executed in (("unit_tests", unit), ("integration_tests", integration)):
        for subtest, name in (mapping.get(field) or {}).items():
            if name not in executed:
                raise ValueError(f"missing required case for {subtest}: {name}")
    field = mapping.get("regression_integration_tests", {})
    if not (set(field.values()) if isinstance(field, dict) else set(field)) <= integration:
        raise ValueError("realtime regression integration evidence missing")
    coverage = release_manifest.release_coverage(contracts.load(output / "coverage.json"), summary["critical_packages"])
    if not any(entry["package"].startswith(contracts.MODULE + "/modules/realtime") for entry in coverage["critical"]) or not any(entry["package"] == contracts.MODULE + "/events/nats" for entry in coverage["critical"]):
        raise ValueError("realtime coverage evidence is missing realtime or events/nats critical packages")
    templates = contracts.load(output / "templates.json")
    if templates.get("status") != "passed":
        raise ValueError("generated profile evidence is incomplete")
    official = evidence_index.read_release_e2e(output, root)
    client_deps = validate_client_dependencies(output, root)
    prepared = (report.get("prepare") or {}).get("realtime") or {}
    if not isinstance(prepared, dict) or not prepared.get("provisioned") or not isinstance(prepared.get("database"), str):
        raise ValueError("realtime release evidence requires recorded realtime fixture preparation")
    drills = contracts.load(output / "release-drills" / "release-drills.json")
    release_drills = report.get("release_drills", {})
    drills_summary = release_manifest.validate_drills(
        drills,
        output / "release-drills",
        report["commit"],
        report["source_sha256"],
        report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"],
        project=release_drills.get("project"),
        host_run=release_drills.get("host_run", False),
        dirty=report.get("working_tree_dirty", False),
    )
    runner = report.get("m7_runner") if isinstance(report.get("m7_runner"), dict) else None
    evidence = {
        "status": "passed",
        "milestone": "M7",
        "baseline": "v1.3",
        "commit": report["commit"],
        "source_sha256": report["source_sha256"],
        "platform": report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"],
        "product_version": summary["versions"]["runtime"],
        "version_scope": contracts.load(root / REALTIME_MANIFEST_REL)["version_scope"],
        "versions": summary["versions"],
        "requirements": summary["requirements"],
        "named_subtests": summary["subtests"],
        "profiles": summary["profiles"],
        "closure": closure,
        "regressions": regressions,
        "critical_packages": summary["critical_packages"],
        "coverage": coverage,
        "template_profiles": sorted({profile["profile"] for profile in templates["profiles"]}),
        "realtime_official_client": {"status": official["status"], "profiles": official["profiles"], "tests": official["tests"], "command": "realtime-e2e"},
        "realtime_prepared": {
            "database": prepared.get("database"),
            "provisioned": True,
            "covered_child_binary": prepared.get("covered_child_binary") is True,
            "child_race": prepared.get("race") is True,
        },
        "client_dependencies": {
            "status": "locked",
            "locked_packages": len(client_deps["packages"]),
            "manifest": "deploy/release/m7-client-dependencies.json",
            "manifest_sha256": contracts.digest(root / "deploy/release/m7-client-dependencies.json"),
            "artifact": "client-dependencies.json",
            "lockfile_sha256": client_deps["lockfile_sha256"],
        },
        "drills": drills_summary,
        "assets": summary["assets"],
        "manifest_sha256": contracts.digest(root / REALTIME_MANIFEST_REL),
        "release_manifest_sha256": contracts.digest(root / release_manifest.MANIFEST_REL),
        "gates": list(release_manifest.GATES),
    }
    if runner:
        evidence["runner_image"] = dict(runner)
    return evidence


def write_release_evidence(evidence, output):
    """Emit the realtime release evidence with canonical LF JSON bytes exactly once."""
    path = Path(output) / evidence_index.RELEASE_EVIDENCE_FILE
    payload = json.dumps(evidence, indent=2, sort_keys=True) + "\n"
    if path.is_file() and path.read_bytes() != payload.encode("utf-8"):
        raise ValueError("release evidence already exists with different content: " + evidence_index.RELEASE_EVIDENCE_FILE)
    path.write_bytes(payload.encode("utf-8"))
    return path


def validate_stored_release_realtime(report, directory, root=ROOT):
    """Reproduce the stored realtime release evidence read-only from raw artifacts."""
    path = Path(directory) / evidence_index.RELEASE_EVIDENCE_FILE
    stored = json.loads(path.read_text(encoding="utf-8-sig"))
    if stored != build_release_realtime(report, directory, root=root):
        raise ValueError("realtime release evidence cannot be reproduced from the raw artifacts")
    if report["artifacts"].get(evidence_index.RELEASE_EVIDENCE_FILE) != contracts.digest(path):
        raise ValueError("realtime release evidence bytes no longer match the recorded artifact hash")
    return stored


def realtime_release_steps(report, output, execute):
    """Injected into the cumulative gate after coverage: release acceptance plus realtime."""
    release_steps(report, output, execute)
    validate_mapping(contracts.load(ROOT / REALTIME_MAPPING_REL), milestone="M7")
    report["inputs"][REALTIME_MAPPING_REL] = contracts.digest(ROOT / REALTIME_MAPPING_REL)
    report["inputs"][REALTIME_MANIFEST_REL] = contracts.digest(ROOT / REALTIME_MANIFEST_REL)
    report["inputs"]["deploy/release/m7-client-dependencies.json"] = contracts.digest(ROOT / "deploy/release/m7-client-dependencies.json")
    if os.getenv("PANGOLIN_M7_RUNNER_IMAGE_ID"):
        report["m7_runner"] = {
            "name": os.environ["PANGOLIN_M7_RUNNER_IMAGE"],
            "id": os.environ["PANGOLIN_M7_RUNNER_IMAGE_ID"],
            "dockerfile_sha256": contracts.digest(ROOT / REALTIME_DOCKERFILE_REL),
        }
    evidence = build_release_realtime(report, output)
    write_release_evidence(evidence, output)
    index = evidence_index.write_index({**report, "status": "passed"}, output, scope="realtime")
    report["m7_release"] = {
        "status": evidence["status"],
        "requirements": evidence["requirements"],
        "named_subtests": evidence["named_subtests"],
        "critical_realtime": sorted({
            entry["package"] for entry in evidence["coverage"]["critical"]
            if entry["package"].startswith(contracts.MODULE + "/modules/realtime") or entry["package"] == contracts.MODULE + "/events/nats"
        }),
        "profile_evidence": {"path": evidence_index.REALTIME_EVIDENCE_FILE, "totals": index["totals"]},
        "release": {"evidence": evidence_index.RELEASE_EVIDENCE_FILE, "drills": [entry["name"] for entry in evidence["drills"]["entries"]]},
    }


def validate_realtime_prepared(report, artifacts):
    """Per-platform realtime fixture evidence, fail-closed on the whole shape."""
    realtime = (report.get("prepare") or {}).get("realtime")
    goos = (report.get("platform") or {}).get("GOOS")
    if (
        not isinstance(realtime, dict)
        or not realtime.get("provisioned")
        or realtime.get("covered_child_binary") is not True
        or realtime.get("race") is not (goos == "linux")
        or not isinstance(realtime.get("database"), str)
        or not REALTIME_DATABASE_FORMAT.fullmatch(realtime["database"])
        or realtime.get("binary") not in artifacts
    ):
        raise ValueError("realtime preparation evidence is missing or forged")


def validate_m4_report(report, commit, directory, mapping, milestone="M4"):
    directory = Path(directory)
    test_runner.validate_report(report, commit, directory)
    if report["milestone"] != milestone or not {"m2-regression", "m3-regression", "m4-regression"} <= {c["name"] for c in report["commands"]}:
        raise ValueError("cumulative M2-M4 regression missing")
    for extra in (contracts.load(ROOT / "tests/contract/m2.json"), contracts.load(ROOT / "tests/contract/m3.json"), mapping):
        for field, key in (("tests", "unit_tests"), ("integration_tests", "integration_tests")):
            if not set(extra[key].values()) <= set(report[field]):
                raise ValueError("required cumulative case evidence missing")
        path = "tests/contract/" + extra["milestone"].lower() + ".json"
        if report["inputs"].get(path) != contracts.digest(ROOT / path):
            raise ValueError("acceptance mapping changed")
    if not {"operations.json", "resilience.json", "drain-normal.json", "drain-deadline.json"} <= report["artifacts"].keys():
        raise ValueError("load/recovery/drain curves missing")
    load = contracts.load(directory / "operations.json")
    if load["records"] != 10000 or load["concurrency"] != 64 or load["reconciled"] != 10000 or len(load["samples"]) < 2:
        raise ValueError("load/recovery reconciliation incomplete")
    resilience = contracts.load(directory / "resilience.json")
    if resilience["sent"] != 4 or resilience["lost"] != 0 or len(resilience["curve"]) < 3:
        raise ValueError("broker recovery reconciliation missing")
    for name, state in (("normal", "succeeded"), ("deadline", "pending")):
        drain = contracts.load(directory / ("drain-" + name + ".json"))
        if not drain["database_last"] or drain["final_state"] != state:
            raise ValueError("drain ordering missing")
    required = {contracts.MODULE + "/tests/m4:" + test for test in ("TestCapacityLoad", "TestActiveHTTPWorkerDrain/normal", "TestActiveHTTPWorkerDrain/deadline", "TestJobAdmissionAndAuthority/revoked-service", "TestJobAdmissionAndAuthority/delegated-user")}
    if not required <= set(report["integration_tests"]):
        raise ValueError("load/drain/authority evidence missing")
    profiles = {contracts.MODULE + "/tests/m3:TestM4PersistentOrganizationJobs/" + name for name in ("organization-enabled", "replacement-contract", "current-grant-revoked")}
    if not profiles <= set(report["integration_tests"]):
        raise ValueError("persistent organization/replacement Job evidence missing")


def validate_m5_report(report, commit, directory, mapping, milestone="M5"):
    directory = Path(directory)
    validate_m4_report(report, commit, directory, contracts.load(ROOT / "tests/contract/m4.json"), milestone=milestone)
    if not {"m5-regression", "template-source", "cli-build", "templates", "cli-coverage"} <= {c["name"] for c in report["commands"]}:
        raise ValueError("generated-template commands missing")
    for field, key in (("tests", "unit_tests"), ("integration_tests", "integration_tests")):
        if not set(mapping[key].values()) <= set(report[field]):
            raise ValueError("M5 case evidence missing")
    if not set(mapping.get("regression_tests", [])) <= set(report["tests"]):
        raise ValueError("JSON configuration regression missing")
    field = mapping.get("regression_integration_tests", {})
    if not (set(field.values()) if isinstance(field, dict) else set(field)) <= set(report["integration_tests"]):
        raise ValueError("regression integration evidence missing")
    path = "tests/contract/m5.json"
    if report["inputs"].get(path) != contracts.digest(ROOT / path):
        raise ValueError("M5 mapping changed")
    if not {"cli.out", "templates.json"} <= report["artifacts"].keys():
        raise ValueError("CLI/template raw evidence missing")
    data = contracts.load(directory / "templates.json")
    if data["status"] != "passed" or data["version"] != "0.1.0" or {p["profile"] for p in data["profiles"]} != {"tenant-only", "organization-enabled"}:
        raise ValueError("both generated profiles required")
    for profile in data["profiles"]:
        if not profile["build_test_start"] or not profile["custom_migration_applied"] or not {"write-tenant-a", "write-tenant-b", "tenant-hint-denied", "owner-dto-rejected", "key-conflict", "semantic-validation"} <= set(profile["checks"]):
            raise ValueError("generated security/UOW contracts missing")
        if profile["profile"] == "organization-enabled" and not {"org-write-2", "org-write-3", "complete-control-plane", "profile-downgrade-blocked"} <= set(profile["checks"]):
            raise ValueError("complete organization template not exercised")
        if not profile["reconciliation"]["force_rls"] or not profile["reconciliation"]["non_privileged_app"] or profile["reconciliation"]["organization_schema"] != (profile["profile"] == "organization-enabled"):
            raise ValueError("generated privilege/RLS reconciliation failed")
        expected = 6 if profile["profile"] == "organization-enabled" else 2
        if any(profile["reconciliation"]["counts"][table] != expected for table in ("pangolin_audit.records", "pangolin_events.outbox", "pangolin_idempotency.results")):
            raise ValueError("generated atomic effects reconciliation missing")
    for command in data["commands"]:
        if command["log"] not in report["artifacts"]:
            raise ValueError("template command log missing")
    if report["platform"]["GOOS"] == "windows":
        if "compose.json" not in report["artifacts"] or "templates-compose" not in {c["name"] for c in report["commands"]}:
            raise ValueError("generated Compose execution missing")
        compose = contracts.load(directory / "compose.json")
        if compose["status"] != "passed" or {p["profile"] for p in compose["profiles"]} != {"tenant", "organization"} or not all(p["ready"] and p["read_write"] for p in compose["profiles"]):
            raise ValueError("both generated Compose profiles required")
    required = contracts.MODULE + "/tests/m3:TestA80PlatformAuthority/"
    if not {required + "current-parent", required + "audit-owner", required + "revoked-ancestry"} <= set(report["integration_tests"]):
        raise ValueError("current ancestry/audit regression missing")


def validate_report(report, commit, directory, scope="release", mapping=None):
    directory = Path(directory)
    if scope not in ("release", "realtime"):
        raise ValueError("unknown verification scope: " + str(scope))
    milestone_label = "M6" if scope == "release" else "M7"
    validate_m5_report(report, commit, directory, contracts.load(ROOT / "tests/contract/m5.json"), milestone=milestone_label)
    commands = {command["name"] for command in report["commands"]}
    required_commands = {"m6-regression", "release-manifest"}
    if not report.get("release_drills", {}).get("host_run"):
        required_commands.add("release-drills")
    if not required_commands <= commands:
        raise ValueError("release acceptance commands missing")
    for path in ("tests/contract/m6.json", "deploy/release/manifest.json"):
        if report["inputs"].get(path) != contracts.digest(ROOT / path):
            raise ValueError("release acceptance input changed after verification: " + path)
    artifacts = {name.replace("\\", "/"): digest for name, digest in report["artifacts"].items()}
    for name in ("release-manifest.json", "release-drills/release-drills.json", "coverage.json", "templates.json", evidence_index.EVIDENCE_FILE):
        if name not in artifacts:
            raise ValueError("raw release evidence missing: " + name)
    evidence = contracts.load(directory / "release-manifest.json")
    release_manifest.validate_evidence(evidence, report, commit, directory)
    if contracts.load(directory / evidence_index.EVIDENCE_FILE) != evidence_index.build_index(report, directory, scope="release"):
        raise ValueError("per-profile evidence index cannot be reproduced from the raw artifacts")
    m6_mapping = mapping if (scope == "release" and mapping is not None) else contracts.load(ROOT / "tests/contract/m6.json")
    if {entry["name"] for entry in evidence["drills"]["entries"]} != set(m6_mapping["drills"].values()):
        raise ValueError("release drill coverage incomplete")
    if evidence["regressions"].get("m6-regression") != len(m6_mapping["python_tests"]):
        raise ValueError("release python acceptance evidence incomplete")
    if scope == "release":
        return
    # Additive realtime acceptance checks.
    map7 = validate_mapping(mapping if mapping is not None else contracts.load(ROOT / REALTIME_MAPPING_REL), milestone="M7")
    for name in (
        "m7-regression", "node-version", "npm-version", "realtime-dbtool", "realtime-db-create",
        "realtime-demo", "realtime-provision", "realtime-broker-cleanup", "realtime-client-deps",
        "realtime-client-licenses", "realtime-e2e", "realtime-coverage", "realtime-db-drop",
    ):
        if not any(command["name"] == name and command["exit_code"] == 0 for command in report["commands"]):
            raise ValueError("realtime acceptance command missing or failed: " + name)
    validate_realtime_prepared(report, artifacts)
    for path in (REALTIME_MAPPING_REL, REALTIME_MANIFEST_REL, "deploy/release/m7-client-dependencies.json"):
        if report["inputs"].get(path) != contracts.digest(ROOT / path):
            raise ValueError("realtime acceptance input changed after verification: " + path)
    for name in (evidence_index.RELEASE_EVIDENCE_FILE, evidence_index.REALTIME_EVIDENCE_FILE, evidence_index.RELEASE_E2E_FILE, "e2e-realtime.out", "client-dependencies.json"):
        if name not in artifacts:
            raise ValueError("realtime raw release evidence missing: " + name)
    validate_client_dependencies(directory)
    e7 = validate_stored_release_realtime(report, directory)
    if contracts.load(directory / evidence_index.REALTIME_EVIDENCE_FILE) != evidence_index.build_index(report, directory, scope="realtime"):
        raise ValueError("realtime per-profile evidence index cannot be reproduced from the raw artifacts")
    if not set(map7["unit_tests"].values()) <= set(report["tests"]):
        raise ValueError("realtime required unit case evidence missing")
    if not set(map7["integration_tests"].values()) <= set(report["integration_tests"]):
        raise ValueError("realtime required integration case evidence missing")
    regression = map7.get("regression_tests", [])
    field = map7.get("regression_integration_tests", {})
    if not set(regression) <= set(report["tests"]) or not (set(field.values()) if isinstance(field, dict) else set(field)) <= set(report["integration_tests"]):
        raise ValueError("realtime regression case evidence missing")
    if list(e7["versions"].values()) != ["0.1.0", "0.1.0", "0.1.0", "v0.1.0"]:
        raise ValueError("realtime must not move the product version")
    if not any(entry["package"].startswith(contracts.MODULE + "/modules/realtime") for entry in e7["coverage"]["critical"]) or not any(entry["package"] == contracts.MODULE + "/events/nats" for entry in e7["coverage"]["critical"]):
        raise ValueError("realtime coverage gate does not enforce the realtime and events/nats critical packages")
    evidence_index.read_release_e2e(directory)
    if e7["commit"] != commit or e7["source_sha256"] != report["source_sha256"]:
        raise ValueError("realtime release evidence is not bound to the verified source")


def compare_release_declarations(windows, linux):
    """Cross-platform agreement on every realtime release declaration."""
    for key in (
        "status", "baseline", "product_version", "version_scope", "versions", "requirements", "named_subtests",
        "profiles", "closure", "regressions", "critical_packages", "template_profiles",
        "realtime_official_client", "client_dependencies",
        "manifest_sha256", "release_manifest_sha256", "gates",
    ):
        if windows[key] != linux[key]:
            raise ValueError("realtime release declarations differ across platforms: " + key)
    for key in ("provisioned", "covered_child_binary"):
        if windows["realtime_prepared"][key] != linux["realtime_prepared"][key]:
            raise ValueError("realtime fixture invariants differ across platforms: " + key)


def gate(commit, scope="release", mapping=None):
    if scope not in ("release", "realtime"):
        raise ValueError("unknown gate scope: " + str(scope))
    stage = "m6" if scope == "release" else "m7"
    root = ROOT / "artifacts" / commit / stage
    reports = {}
    for label in ("windows-amd64", "linux-amd64"):
        directory = root / label
        report = contracts.load(directory / "report.json")
        validate_report(report, commit, directory, scope=scope, mapping=mapping)
        if report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"] != label:
            raise ValueError("wrong evidence platform")
        reports[label] = report
    if reports["windows-amd64"]["source_sha256"] != reports["linux-amd64"]["source_sha256"]:
        raise ValueError("different source trees")
    if scope == "release":
        m6_mapping = mapping or contracts.load(ROOT / "tests/contract/m6.json")
        evidence = {label: contracts.load(root / label / "release-manifest.json") for label in reports}
        for label, report in reports.items():
            if evidence[label]["commit"] != commit or evidence[label]["source_sha256"] != report["source_sha256"]:
                raise ValueError("release evidence is not bound to the verified source: " + label)
        for key in (
            "status", "baseline", "product_version", "version_scope", "versions", "event_schemas",
            "requirements", "subtests", "deliveries", "profiles", "closure", "regressions",
            "template_bundle", "template_profiles", "manifest_sha256", "gates",
        ):
            if evidence["windows-amd64"][key] != evidence["linux-amd64"][key]:
                raise ValueError("release declarations differ across platforms: " + key)
        result = {
            "milestone": "M6",
            "status": "verified",
            "source_commit": commit,
            "source_sha256": reports["windows-amd64"]["source_sha256"],
            "baseline": "v1.3",
            "named_subtests": sum(len(m6_mapping[key]) for key in M6_FIELDS),
            "total_named_subtests": evidence["windows-amd64"]["subtests"],
            "requirements": evidence["windows-amd64"]["requirements"],
            "reports": {label: contracts.digest(root / label / "report.json") for label in reports},
            "profile_evidence": {label: contracts.digest(root / label / evidence_index.EVIDENCE_FILE) for label in reports},
            "release_manifest": {label: contracts.digest(root / label / "release-manifest.json") for label in reports},
        }
        (root / "gate.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
        print(json.dumps(result, indent=2))
        return result
    m7_mapping = mapping or contracts.load(ROOT / REALTIME_MAPPING_REL)
    evidence = {label: contracts.load(root / label / evidence_index.RELEASE_EVIDENCE_FILE) for label in reports}
    for label, report in reports.items():
        if evidence[label]["commit"] != commit or evidence[label]["source_sha256"] != report["source_sha256"]:
            raise ValueError("realtime evidence is not bound to the verified source: " + label)
    compare_release_declarations(evidence["windows-amd64"], evidence["linux-amd64"])
    result = {
        "milestone": "M7",
        "status": "verified",
        "source_commit": commit,
        "source_sha256": reports["windows-amd64"]["source_sha256"],
        "baseline": "v1.3",
        "named_subtests": sum(len(m7_mapping[key]) for key in M7_FIELDS),
        "total_named_subtests": evidence_index.REALTIME_SUBTESTS,
        "requirements": evidence_index.BASELINE_REQUIREMENTS + evidence_index.REALTIME_REQUIREMENTS,
        "reports": {label: contracts.digest(root / label / "report.json") for label in reports},
        "profile_evidence": {label: contracts.digest(root / label / evidence_index.REALTIME_EVIDENCE_FILE) for label in reports},
        "release_m7": {label: contracts.digest(root / label / evidence_index.RELEASE_EVIDENCE_FILE) for label in reports},
        "release_manifest": {label: contracts.digest(root / label / "release-manifest.json") for label in reports},
    }
    (root / "gate.json").write_bytes((json.dumps(result, indent=2) + "\n").encode("utf-8"))
    print(json.dumps(result, indent=2))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["verify", "gate"])
    parser.add_argument("--project")
    parser.add_argument("--commit")
    parser.add_argument("--race", action="store_true")
    parser.add_argument("--scope", choices=["release", "realtime"], default="release")
    parser.add_argument("--external-broker-fixture", action="store_true")
    args = parser.parse_args()
    mapping_path = ROOT / ("tests/contract/m6.json" if args.scope == "release" else REALTIME_MAPPING_REL)
    milestone = "M6" if args.scope == "release" else "M7"
    mapping = validate_mapping(contracts.load(mapping_path), milestone=milestone)
    if args.action == "gate":
        if not args.commit or not re.fullmatch(r"[0-9a-f]{40}", args.commit):
            parser.error("gate requires full --commit")
        gate(args.commit, scope=args.scope, mapping=mapping)
        return 0
    if not args.project:
        parser.error("verify requires --project")
    os.environ.update(database_env())
    prepare_pg_clients(args.project)
    verify_kwargs = {"increment": mapping, "extra_steps": release_steps}
    if args.scope == "realtime":
        verify_kwargs.update(extra_steps=realtime_release_steps, coverage_steps=realtime_coverage_steps, prepare=prepare_realtime)
    if args.external_broker_fixture:
        if not os.getenv("PANGOLIN_M3_BROKER_RESTART_DIR"):
            parser.error("external broker fixture requires barrier directory")
        return test_runner.verify(args, **verify_kwargs)
    barrier = ROOT / "artifacts" / ("release-broker-barrier" if args.scope == "release" else "realtime-broker-barrier")
    with nats_fixture.broker_restart(args.project, barrier) as directory:
        os.environ["PANGOLIN_M3_BROKER_RESTART_DIR"] = str(directory)
        return test_runner.verify(args, **verify_kwargs)


if __name__ == "__main__":
    raise SystemExit(main())
