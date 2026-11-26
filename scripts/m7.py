"""M7 full same-source realtime acceptance: cumulative gates plus the official-client E2E.

M7 reuses every M0-M6 engineering gate through scripts/m1.py (inventory,
build/vet/module-verify, unit + regression + real-service integration suites,
E2E, cumulative coverage union, race, sqlc) and the M6 release acceptance
through m6.release_steps (release manifest, executable drills, bound M6
evidence), then adds the fail-closed M7 layer on the same source bytes:

- the official @microsoft/signalr 10.0.0 client harness (three release
  profiles, two independent instrumented realtime nodes on real pinned
  PostgreSQL/NATS) contributing a raw atomic coverage profile to the union,
- the CLI-generated profiles via the M5 template run, all merged before the
  release coverage gate enforces >=90% raw statement coverage overall and for
  every critical package including modules/realtime/... and events/nats,
- the additive release manifest deploy/release/m7.json (104 requirements / 593
  named subtests; the VERSION stays 0.1.0: the v0.2 realtime subsystem is not
  a tag release),
- the specification 13.6 per-profile evidence index for all 104 requirements
  across the three profile plans (scripts/m7_index.py).

Nothing is claimed Verified until the same-commit Windows and pinned Linux
reports both reproduce this evidence and the M6 acceptance inside them.
"""
import argparse
import json
import os
import sys
import uuid
from pathlib import Path
import re
import shutil

import m0
import m1
import m3_fixture
import m5
import m6
import m6_index
import m7_index
import release_manifest

ROOT = m0.ROOT
FIELDS = (*m7_index.FIELDS, "gate_checks")
MANIFEST_REL = "deploy/release/m7.json"
DOCKERFILE_REL = "deploy/docker/Dockerfile.m7-runner"
MAPPING_REL = "tests/contract/m7.json"
GATE_CONTRACT = m7_index.GATE_CONTRACT


def validate_mapping(mapping):
    """Fail closed unless every M7 named subtest is mapped and nothing is pending."""
    groups = m0.load(ROOT / "tests/contract/subtests.json")["groups"]
    expected = {subtest["id"] for group in groups if group["milestone"] == "M7" for subtest in group["subtests"]}
    sets = [set(mapping.get(key) or {}) for key in FIELDS]
    if mapping["milestone"] != "M7" or mapping["baseline"] != "v1.3" or set.union(*sets) != expected or sum(map(len, sets)) != len(expected):
        raise ValueError("M7 acceptance mapping incomplete or duplicated")
    if mapping.get("pending"):
        raise ValueError("M7 has unimplemented acceptance: " +
                         ", ".join(entry.get("id", "?") for entry in mapping["pending"]))
    if any(not identity for key in FIELDS for identity in (mapping.get(key) or {}).values()):
        raise ValueError("M7 has empty acceptance identities")
    for subtest, identity in (mapping.get("gate_checks") or {}).items():
        if identity != m7_index.GATE_CONTRACT:
            raise ValueError("M7 gate_checks identity must bind " + m7_index.GATE_CONTRACT + ": " + subtest)
    for field in ("unit_tests", "integration_tests"):
        for subtest, identity in (mapping.get(field) or {}).items():
            if not re.fullmatch(m0.MODULE.replace(".", r"\.") + r"/[A-Za-z0-9_./-]+:Test[A-Za-z0-9_/.-]+", identity):
                raise ValueError("M7 " + field + " identity is not a bound Go test: " + subtest + " -> " + identity)
    for subtest, identity in (mapping.get("python_tests") or {}).items():
        if not re.fullmatch(r"[A-Za-z0-9_.]+", identity):
            raise ValueError("M7 python_tests identity is not an unittest name: " + subtest)
    for subtest, identity in (mapping.get("template_tests") or {}).items():
        if identity not in (m7_index.REALTIME_CONTRACT, m6_index.TEMPLATE_CONTRACT):
            raise ValueError("M7 template identity is not a supported contract: " + subtest + " -> " + identity)
    for field in ("regression_tests", "regression_integration_tests"):
        field_value = mapping.get(field)
        names = field_value.values() if isinstance(field_value, dict) else (field_value or [])
        if any(not isinstance(name, str) or not name for name in names):
            raise ValueError("M7 regression mapping incomplete")
    return mapping


def _realtime_env_names():
    return ("PANGOLIN_M1_ADMIN_DSN", "PANGOLIN_M1_APP_DSN", "PANGOLIN_M1_WORKER_DSN",
            "PANGOLIN_M1_NATS_URL", "PANGOLIN_M1_ISSUER", "PANGOLIN_M7_DEMO_KEY")


def replace_database(dsn, database):
    from urllib.parse import urlsplit, urlunsplit
    parts = urlsplit(dsn)
    return urlunsplit(parts._replace(path="/" + database))


def prepare_realtime(report, output, execute, packages):
    """Same-source realtime fixture, recorded before the unit suite runs.

    Creates a dedicated realtime database inside the isolated project's
    pinned PostgreSQL (never the pre-migrated M1/M3 fixture database), builds
    the covered demo binary (with -race whenever this run also races), maps
    the PANGOLIN_REALTIME_* environment the acceptance tests require, and
    initializes the schema and demo credentials through examples/realtime
    --provision. The binary, environment and coverage directory stay active
    for unit, integration, race and the official-client E2E, so every
    subprocess run joins the same-source raw coverage union; missing pieces
    fail closed instead of skipping tests.
    """
    output = Path(output)
    missing = [name for name in _realtime_env_names() if not os.getenv(name)]
    if missing:
        raise ValueError("M7 realtime preparation environment missing: " + ", ".join(missing))
    covered = output / "realtime-cov"
    covered.mkdir(exist_ok=True)
    platform = report["platform"]
    race = bool(report.get("requested_race")) and platform["GOOS"] == "linux"
    db_tool = output / ("realtime-db.exe" if os.name == "nt" else "realtime-db")
    execute(["go", "build", "-o", str(db_tool), "./internal/testkit/cmd/template-db"], "realtime-dbtool")
    # The template-db fixture tool owns database creation inside the isolated
    # project's PostgreSQL; the dedicated M7 database keeps its m7rt prefix so
    # it can never collide with the pre-migrated M1/M3 fixture database.
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
        "PANGOLIN_REALTIME_NATS_URL": os.environ["PANGOLIN_M1_NATS_URL"]})
    execute(["go", "run", "./examples/realtime", "--provision"], "realtime-provision")
    # --provision creates the demo migrations/schema and its default stream;
    # that default PANGOLIN_REALTIME_DEMO stream is never used by the suites
    # and each demo stream reservation is 64 MiB of the pinned NATS budget, so
    # the fixture CLI deletes exactly that stream (literal route verified in
    # the binary) right after provisioning.
    execute([str(binary), "--cleanup-stream", "PANGOLIN_REALTIME_DEMO"], "realtime-broker-cleanup")
    return {"realtime": {"database": database, "database_tool": db_tool.name,
                         "binary": binary.name, "provisioned": True, "race": race,
                         "coverage_dir": covered.name, "covered_child_binary": True}}


def realtime_coverage_steps(report, output, execute, packages):
    """Official-client E2E on the prepared fixture; returns the coverage profile.

    Reuses the prepared covered binary, dedicated database, environment and
    GOCOVERDIR from prepare_realtime — the three official @microsoft/signalr
    10.0.0 profile harnesses drive two independent instrumented nodes on real
    PostgreSQL/NATS, and every child counter joins the same-source raw union.
    """
    output = Path(output)
    prepared = (report.get("prepare") or {}).get("realtime")
    if not isinstance(prepared, dict) or not prepared.get("provisioned"):
        raise ValueError("M7 realtime fixture was not prepared before the acceptance suites")
    node = shutil.which("node")
    npm = shutil.which("npm.cmd" if os.name == "nt" else "npm")
    if not node or not npm:
        raise ValueError("official-client harness requires Node.js and npm on the runner")
    missing = [name for name in ("PANGOLIN_REALTIME_BINARY", "PANGOLIN_REALTIME_ADMIN_DSN", "PANGOLIN_REALTIME_APP_DSN",
                                 "PANGOLIN_REALTIME_WORKER_DSN", "PANGOLIN_REALTIME_NATS_URL",
                                 "PANGOLIN_REALTIME_DEMO_KEY") if not os.getenv(name)]
    if missing:
        raise ValueError("M7 realtime acceptance environment missing: " + ", ".join(missing))
    binary = Path(os.environ["PANGOLIN_REALTIME_BINARY"])
    if not binary.is_file():
        raise ValueError("prepared realtime binary is missing: " + str(binary))
    covered = output / prepared["coverage_dir"]
    execute(["node", "--version"], "node-version")
    execute([npm, "--version"], "npm-version")
    execute([npm, "--prefix", "examples/realtime/client", "ci", "--ignore-scripts"], "realtime-client-deps")
    # The npm tree the harness will load is re-derived against the reviewed
    # dependency/license manifest and persisted as raw gate evidence.
    execute([sys.executable, "scripts/m7_client_dependencies.py", "--check",
             "--output", str(output / "client-dependencies.json")], "realtime-client-licenses")
    stream = execute([npm, "--prefix", "examples/realtime/client", "test", "--", "--test-reporter=tap"], "realtime-e2e")
    parsed = m7_index.parse_official_log(stream)
    raw = {"kind": "official-client-harness", "status": "passed", "command": "realtime-e2e",
           "tests": parsed["tests"], "skipped": 0, "failed": 0, "profiles": parsed["profiles"]}
    (output / m7_index.RELEASE_E2E_FILE).write_text(json.dumps(raw, indent=2) + "\n", encoding="utf-8")
    if not list(covered.glob("covmeta.*")) or not list(covered.glob("covcounters.*")):
        raise ValueError("official-client harness produced no coverage instrumentation")
    execute(["go", "tool", "covdata", "textfmt", "-i=" + str(covered), "-o=" + str(output / "e2e-realtime.out")], "realtime-coverage")
    db_tool = output / prepared["database_tool"]
    execute([str(db_tool), "drop", str(prepared["database"])], "realtime-db-drop")
    return [output / "e2e-realtime.out"]


def validate_m7_manifest(manifest, root=ROOT):
    """Validate the additive M7 release manifest against every real source."""
    root = Path(root)
    keys = ("milestone", "baseline", "product_version", "version_scope", "requirements", "subtests",
            "profiles", "critical_packages", "drills", "gates", "version_bindings", "assets")
    unknown = sorted(set(manifest) - set(keys))
    missing = [key for key in keys if key not in manifest]
    if missing or unknown:
        raise ValueError("M7 release manifest keys missing or unknown: " + ", ".join(missing + unknown))
    if manifest["milestone"] != "M7" or manifest["baseline"] != "v1.3":
        raise ValueError("M7 release manifest baseline drift")
    if manifest["product_version"] != release_manifest.PRODUCT_VERSION or manifest["version_scope"] != release_manifest.VERSION_SCOPE:
        raise ValueError("M7 keeps VERSION at " + release_manifest.PRODUCT_VERSION + ": the realtime subsystem is not a tag release")
    if manifest["requirements"] != m7_index.BASELINE_REQUIREMENTS + m7_index.REALTIME_REQUIREMENTS or manifest["subtests"] != m7_index.NAMED_SUBTESTS:
        raise ValueError("M7 release manifest must bind " + str(m7_index.BASELINE_REQUIREMENTS + m7_index.REALTIME_REQUIREMENTS) +
                         " requirements and " + str(m7_index.NAMED_SUBTESTS) + " named subtests")
    if [profile["profile"] for profile in manifest["profiles"]] != list(release_manifest.PROFILES):
        raise ValueError("M7 release manifest profiles must list all three applicability rows")
    for profile in manifest["profiles"]:
        if set(profile) != {"profile", "applicability_source"} or \
                profile["applicability_source"] != f"tests/contract/profiles/{profile['profile']}.json":
            raise ValueError("M7 profile applicability source drift: " + profile.get("profile", "?"))
    required = set(release_manifest.load_manifest(root)["critical_packages"]) | {"modules/realtime", "events/nats"}
    if sorted(set(manifest["critical_packages"])) != sorted(required):
        raise ValueError("M7 critical packages must match the historical release list plus realtime and events/nats")
    if set(manifest["drills"]) != set(release_manifest.DRILLS) or manifest["gates"] != list(release_manifest.GATES):
        raise ValueError("M7 release manifest must reuse the five executable drills and both verified platforms")
    versions = release_manifest.check_versions(manifest, root)
    assets = manifest["assets"]
    if not assets or len(set(assets)) != len(assets) or assets[0] != MANIFEST_REL:
        raise ValueError("M7 release assets must be a non-empty unique list starting with the manifest itself")
    required_assets = (MANIFEST_REL, "deploy/release/m7-client-dependencies.json", "docs/guides/m7-development.md", DOCKERFILE_REL)
    if any(name not in assets for name in required_assets):
        raise ValueError("missing M7 release asset(s): " + ", ".join(name for name in required_assets if name not in assets))
    for name in assets:
        path = (Path(root) / name).resolve()
        if not path.is_relative_to(Path(root).resolve()) or not path.is_file() or path.stat().st_size == 0:
            raise ValueError("missing M7 release asset: " + name)
    return {"requirements": manifest["requirements"], "subtests": manifest["subtests"],
            "profiles": release_manifest.profile_rows(root), "versions": versions,
            "critical_packages": sorted(set(manifest["critical_packages"])),
            "assets": {name: m0.digest(Path(root) / name) for name in assets}}


def validate_client_dependencies(output, root=ROOT):
    """Re-derive the raw client-dependencies artifact against the reviewed manifest.

    The gate re-reads the recorded raw inventory JSON from the report
    directory and re-validates the actual packages, versions, resolved URLs,
    integrity hashes and license-file digests against the committed
    deploy/release/m7-client-dependencies.json — passing metadata alone is
    never accepted, a drifted artifact fails its own re-derivation.
    """
    import m7_client_dependencies
    root, output = Path(root), Path(output)
    manifest = m0.load(root / "deploy/release/m7-client-dependencies.json")
    artifact = m0.load(output / "client-dependencies.json")
    m7_client_dependencies.validate(manifest, artifact)
    return artifact


def build_release_m7(report, output, root=ROOT):
    """Collect and write release-m7.json: the additive M7 release evidence."""
    root, output = Path(root), Path(output)
    summary = validate_m7_manifest(m0.load(root / MANIFEST_REL), root)
    groups = m0.load(root / "tests/contract/subtests.json")["groups"]
    mappings = {number: m0.load(root / f"tests/contract/m{number}.json") for number in range(1, 8)}
    mapping = mappings[max(mappings)]
    closure = release_manifest.check_closure(mappings, groups)
    if closure["mapped"] != m7_index.NAMED_SUBTESTS or closure["milestones"].get("M7") != m7_index.REALTIME_NAMED:
        raise ValueError("M7 release closure is incomplete: " + str(closure["milestones"]))
    regressions = release_manifest.validate_regression_logs(report, output, extra={"m7-regression": set()})
    unit, integration = set(report["tests"]), set(report["integration_tests"])
    for field, executed in (("unit_tests", unit), ("integration_tests", integration)):
        for subtest, name in (mapping.get(field) or {}).items():
            if name not in executed:
                raise ValueError(f"missing required case for {subtest}: {name}")
    field = mapping.get("regression_integration_tests", {})
    if not (set(field.values()) if isinstance(field, dict) else set(field)) <= integration:
        raise ValueError("M7 regression integration evidence missing")
    coverage = release_manifest.release_coverage(m0.load(output / "coverage.json"), summary["critical_packages"])
    if not any(entry["package"].startswith(m0.MODULE + "/modules/realtime") for entry in coverage["critical"]) \
            or not any(entry["package"] == m0.MODULE + "/events/nats" for entry in coverage["critical"]):
        raise ValueError("M7 coverage evidence is missing realtime or events/nats critical packages")
    templates = m0.load(output / "templates.json")
    if templates.get("status") != "passed":
        raise ValueError("generated profile evidence is incomplete")
    official = m7_index.read_release_e2e(output, root)
    client_dependencies = validate_client_dependencies(output, root)
    prepared = (report.get("prepare") or {}).get("realtime") or {}
    if not isinstance(prepared, dict) or not prepared.get("provisioned") \
            or not isinstance(prepared.get("database"), str):
        raise ValueError("M7 release evidence requires recorded realtime fixture preparation")
    drills = m0.load(output / "release-drills" / "release-drills.json")
    release_drills = report.get("release_drills", {})
    drills_summary = release_manifest.validate_drills(drills, output / "release-drills", report["commit"], report["source_sha256"],
                                                      report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"],
                                                      project=release_drills.get("project"), host_run=release_drills.get("host_run", False),
                                                      dirty=report.get("working_tree_dirty", False))
    runner = report.get("m7_runner") if isinstance(report.get("m7_runner"), dict) else None
    evidence = {
        "status": "passed", "milestone": "M7", "baseline": "v1.3",
        "commit": report["commit"], "source_sha256": report["source_sha256"],
        "platform": report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"],
        "product_version": summary["versions"]["runtime"], "version_scope": m0.load(root / MANIFEST_REL)["version_scope"],
        "versions": summary["versions"], "requirements": summary["requirements"], "named_subtests": summary["subtests"],
        "profiles": summary["profiles"], "closure": closure, "regressions": regressions,
        "critical_packages": summary["critical_packages"], "coverage": coverage,
        "template_profiles": sorted({profile["profile"] for profile in templates["profiles"]}),
        "realtime_official_client": {"status": official["status"], "profiles": official["profiles"],
                                     "tests": official["tests"], "command": "realtime-e2e"},
        "realtime_prepared": {"database": prepared.get("database"), "provisioned": True,
                              "covered_child_binary": prepared.get("covered_child_binary") is True,
                              "child_race": prepared.get("race") is True},
        "client_dependencies": {"status": "locked", "locked_packages": len(client_dependencies["packages"]),
                                "manifest": "deploy/release/m7-client-dependencies.json",
                                "manifest_sha256": m0.digest(root / "deploy/release/m7-client-dependencies.json"),
                                "artifact": "client-dependencies.json",
                                "lockfile_sha256": client_dependencies["lockfile_sha256"]},
        "drills": drills_summary, "assets": summary["assets"],
        "manifest_sha256": m0.digest(root / MANIFEST_REL),
        "release_manifest_sha256": m0.digest(root / release_manifest.MANIFEST_REL),
        "gates": list(release_manifest.GATES),
    }
    if runner:
        evidence["runner_image"] = dict(runner)
    (output / m7_index.RELEASE_EVIDENCE_FILE).write_text(json.dumps(evidence, indent=2) + "\n", encoding="utf-8")
    return evidence


def release_steps(report, output, execute):
    """Injected into the cumulative gate after coverage: M6 release acceptance plus M7."""
    m6.release_steps(report, output, execute)
    validate_mapping(m0.load(ROOT / MAPPING_REL))
    report["inputs"][MAPPING_REL] = m0.digest(ROOT / MAPPING_REL)
    report["inputs"][MANIFEST_REL] = m0.digest(ROOT / MANIFEST_REL)
    report["inputs"]["deploy/release/m7-client-dependencies.json"] = m0.digest(ROOT / "deploy/release/m7-client-dependencies.json")
    if os.getenv("PANGOLIN_M7_RUNNER_IMAGE_ID"):
        report["m7_runner"] = {"name": os.environ["PANGOLIN_M7_RUNNER_IMAGE"],
                               "id": os.environ["PANGOLIN_M7_RUNNER_IMAGE_ID"],
                               "dockerfile_sha256": m0.digest(ROOT / DOCKERFILE_REL)}
    evidence = build_release_m7(report, output)
    # The cumulative runner finalizes report status/artifacts after this hook;
    # like the M6 index, the M7 index is written from the passing projection
    # and strictly re-derived from the finished report during gate validation.
    index = m7_index.write_index({**report, "status": "passed"}, output)
    report["m7_release"] = {"status": evidence["status"], "requirements": evidence["requirements"],
                            "named_subtests": evidence["named_subtests"],
                            "critical_realtime": sorted({entry["package"] for entry in evidence["coverage"]["critical"]
                                                         if entry["package"].startswith(m0.MODULE + "/modules/realtime")
                                                         or entry["package"] == m0.MODULE + "/events/nats"}),
                            "profile_evidence": {"path": m7_index.EVIDENCE_FILE, "totals": index["totals"]},
                            "release": {"evidence": m7_index.RELEASE_EVIDENCE_FILE, "drills": [entry["name"] for entry in evidence["drills"]["entries"]]}}


REALTIME_DATABASE_FORMAT = re.compile(r"m5_template_m7rt_[0-9a-f]{12}")


def validate_realtime_prepared(report, artifacts):
    """Per-platform realtime fixture evidence, fail-closed on the whole shape.

    The dedicated database name is random per run and the child demo binary
    races only on Linux (bool(requested_race) and GOOS == linux), so both
    deliberately differ across platforms; they are validated strictly per
    report here and never compared cross-platform by the M7 gate."""
    realtime = (report.get("prepare") or {}).get("realtime")
    goos = (report.get("platform") or {}).get("GOOS")
    if not isinstance(realtime, dict) \
            or not realtime.get("provisioned") or realtime.get("covered_child_binary") is not True \
            or realtime.get("race") is not (goos == "linux") \
            or not isinstance(realtime.get("database"), str) \
            or not REALTIME_DATABASE_FORMAT.fullmatch(realtime["database"]) \
            or realtime.get("binary") not in artifacts:
        raise ValueError("M7 realtime preparation evidence is missing or forged")


def validate_report(report, commit, directory):
    directory = Path(directory)
    # Cumulative M1..M5 chain revalidation with the M7 report label.
    m5.validate_report(report, commit, directory, m0.load(ROOT / "tests/contract/m5.json"), milestone="M7")
    mapping = m0.load(ROOT / MAPPING_REL)
    commands = {command["name"] for command in report["commands"]}
    required_commands = {"m6-regression", "release-manifest"}
    if not report.get("release_drills", {}).get("host_run"):
        required_commands.add("release-drills")
    if not required_commands <= commands:
        raise ValueError("M6 release acceptance commands missing")
    for path in ("tests/contract/m6.json", "deploy/release/manifest.json"):
        if report["inputs"].get(path) != m0.digest(ROOT / path):
            raise ValueError("M6 acceptance input changed after verification: " + path)
    artifacts = {name.replace("\\", "/"): digest for name, digest in report["artifacts"].items()}
    for name in ("release-manifest.json", "release-drills/release-drills.json", "coverage.json", "templates.json", m6_index.EVIDENCE_FILE):
        if name not in artifacts:
            raise ValueError("M6 raw release evidence missing: " + name)
    evidence = m0.load(directory / "release-manifest.json")
    release_manifest.validate_evidence(evidence, report, commit, directory)
    if m0.load(directory / m6_index.EVIDENCE_FILE) != m6_index.build_index(report, directory):
        raise ValueError("M6 per-profile evidence index cannot be reproduced from the raw artifacts")
    m6_mapping = m0.load(ROOT / "tests/contract/m6.json")
    if {entry["name"] for entry in evidence["drills"]["entries"]} != set(m6_mapping["drills"].values()):
        raise ValueError("M6 drill coverage incomplete")
    if evidence["regressions"].get("m6-regression") != len(m6_mapping["python_tests"]):
        raise ValueError("M6 python acceptance evidence incomplete")
    # M7 additive acceptance.
    map7 = validate_mapping(mapping)
    for name in ("m7-regression", "node-version", "npm-version", "realtime-dbtool", "realtime-db-create",
                 "realtime-demo", "realtime-provision", "realtime-broker-cleanup", "realtime-client-deps",
                 "realtime-client-licenses", "realtime-e2e",
                 "realtime-coverage", "realtime-db-drop"):
        if not any(command["name"] == name and command["exit_code"] == 0 for command in report["commands"]):
            raise ValueError("M7 acceptance command missing or failed: " + name)
    validate_realtime_prepared(report, artifacts)
    for path in (MAPPING_REL, MANIFEST_REL, "deploy/release/m7-client-dependencies.json"):
        if report["inputs"].get(path) != m0.digest(ROOT / path):
            raise ValueError("M7 acceptance input changed after verification: " + path)
    for name in (m7_index.RELEASE_EVIDENCE_FILE, m7_index.EVIDENCE_FILE, m7_index.RELEASE_E2E_FILE, "e2e-realtime.out",
                 "client-dependencies.json"):
        if name not in artifacts:
            raise ValueError("M7 raw release evidence missing: " + name)
    validate_client_dependencies(directory)
    e7 = build_release_m7(report, directory)
    if m0.load(directory / m7_index.RELEASE_EVIDENCE_FILE) != e7:
        raise ValueError("M7 release evidence cannot be reproduced from the raw artifacts")
    if m0.load(directory / m7_index.EVIDENCE_FILE) != m7_index.build_index(report, directory):
        raise ValueError("M7 per-profile evidence index cannot be reproduced from the raw artifacts")
    if not set(map7["unit_tests"].values()) <= set(report["tests"]):
        raise ValueError("M7 required unit case evidence missing")
    if not set(map7["integration_tests"].values()) <= set(report["integration_tests"]):
        raise ValueError("M7 required integration case evidence missing")
    regression = map7.get("regression_tests", [])
    field = map7.get("regression_integration_tests", {})
    if not set(regression) <= set(report["tests"]) or not (set(field.values()) if isinstance(field, dict) else set(field)) <= set(report["integration_tests"]):
        raise ValueError("M7 regression case evidence missing")
    if list(e7["versions"].values()) != ["0.1.0", "0.1.0", "0.1.0", "v0.1.0"]:
        raise ValueError("M7 must not move the product version")
    if not any(entry["package"].startswith(m0.MODULE + "/modules/realtime") for entry in e7["coverage"]["critical"]) \
            or not any(entry["package"] == m0.MODULE + "/events/nats" for entry in e7["coverage"]["critical"]):
        raise ValueError("M7 coverage gate does not enforce the realtime and events/nats critical packages")
    m7_index.read_release_e2e(directory)
    if e7["commit"] != commit or e7["source_sha256"] != report["source_sha256"]:
        raise ValueError("M7 release evidence is not bound to the verified source")


def compare_release_declarations(windows, linux):
    """Cross-platform agreement on every release declaration.

    realtime_prepared carries the per-run random database name and the
    intentionally Linux-only child race, so only its platform-independent
    invariants (provisioned, covered_child_binary) are compared here; the
    platform-specific fields stay validated per report instead."""
    for key in ("status", "baseline", "product_version", "version_scope", "versions", "requirements", "named_subtests",
                "profiles", "closure", "regressions", "critical_packages", "template_profiles",
                "realtime_official_client", "client_dependencies",
                "manifest_sha256", "release_manifest_sha256", "gates"):
        if windows[key] != linux[key]:
            raise ValueError("M7 release declarations differ across platforms: " + key)
    for key in ("provisioned", "covered_child_binary"):
        if windows["realtime_prepared"][key] != linux["realtime_prepared"][key]:
            raise ValueError("M7 realtime fixture invariants differ across platforms: " + key)


def gate(commit, mapping):
    root = ROOT / "artifacts" / commit / "m7"
    reports = {}
    for label in ("windows-amd64", "linux-amd64"):
        directory = root / label
        report = m0.load(directory / "report.json")
        validate_report(report, commit, directory)
        if report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"] != label:
            raise ValueError("wrong evidence platform")
        reports[label] = report
    if reports["windows-amd64"]["source_sha256"] != reports["linux-amd64"]["source_sha256"]:
        raise ValueError("different source trees")
    evidence = {label: m0.load(root / label / m7_index.RELEASE_EVIDENCE_FILE) for label in reports}
    for label, report in reports.items():
        if evidence[label]["commit"] != commit or evidence[label]["source_sha256"] != report["source_sha256"]:
            raise ValueError("M7 evidence is not bound to the verified source: " + label)
    compare_release_declarations(evidence["windows-amd64"], evidence["linux-amd64"])
    result = {"milestone": "M7", "status": "verified", "source_commit": commit,
              "source_sha256": reports["windows-amd64"]["source_sha256"], "baseline": "v1.3",
              "named_subtests": sum(len(mapping[key]) for key in FIELDS),
              "total_named_subtests": m7_index.NAMED_SUBTESTS,
              "requirements": m7_index.BASELINE_REQUIREMENTS + m7_index.REALTIME_REQUIREMENTS,
              "reports": {label: m0.digest(root / label / "report.json") for label in reports},
              "profile_evidence": {label: m0.digest(root / label / m7_index.EVIDENCE_FILE) for label in reports},
              "release_m7": {label: m0.digest(root / label / m7_index.RELEASE_EVIDENCE_FILE) for label in reports},
              "release_manifest": {label: m0.digest(root / label / "release-manifest.json") for label in reports}}
    (root / "gate.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["verify", "gate"])
    parser.add_argument("--project")
    parser.add_argument("--commit")
    parser.add_argument("--race", action="store_true")
    parser.add_argument("--external-broker-fixture", action="store_true")
    args = parser.parse_args()
    mapping = validate_mapping(m0.load(ROOT / MAPPING_REL))
    if args.action == "gate":
        if not args.commit or not re.fullmatch(r"[0-9a-f]{40}", args.commit):
            parser.error("gate requires full --commit")
        gate(args.commit, mapping)
        return 0
    if not args.project:
        parser.error("verify requires --project")
    os.environ.update(m5.database_env())
    m6.prepare_pg_clients(args.project)
    if args.external_broker_fixture:
        if not os.getenv("PANGOLIN_M3_BROKER_RESTART_DIR"):
            parser.error("external broker fixture requires barrier directory")
        return m1.verify(args, increment=mapping, extra_steps=release_steps, coverage_steps=realtime_coverage_steps, prepare=prepare_realtime)
    with m3_fixture.broker_restart(args.project, ROOT / "artifacts" / "m7-broker-barrier", milestone=7) as directory:
        os.environ["PANGOLIN_M3_BROKER_RESTART_DIR"] = str(directory)
        return m1.verify(args, increment=mapping, extra_steps=release_steps, coverage_steps=realtime_coverage_steps, prepare=prepare_realtime)


if __name__ == "__main__":
    raise SystemExit(main())
