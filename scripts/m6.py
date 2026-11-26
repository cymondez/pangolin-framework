"""M6 full release acceptance: cumulative gates, executable drills, bound manifest.

M6 reuses every M0-M5 cumulative engineering gate, real-service suite,
database correction and generated-profile check, then adds the fail-closed
release acceptance: the committed release manifest, the five executable
release drills and machine-collected release evidence bound to the same
clean commit and source bytes. The official milestone status stays In
progress until the same-commit two-platform gate passes.
"""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

import m0
import m1
import m3_fixture
import m5
import m6_index
import release_manifest

ROOT = m0.ROOT
FIELDS = ("unit_tests", "integration_tests", "python_tests", "drills")


def prepare_pg_clients(project):
    """Select clients before cumulative integration and race, not only drills."""
    mode = os.getenv("PANGOLIN_M6_PG_MODE", "")
    if mode:
        if mode not in ("host", "container"):
            raise ValueError("unknown M6 PostgreSQL client mode")
        if mode == "container" and not os.getenv("PANGOLIN_M6_PG_CONTAINER_ARGV"):
            raise ValueError("container PostgreSQL clients require a command prefix")
        return
    major = int(m0.load(ROOT / "deploy/toolchain.json")["postgres"].split(".")[0])
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
        if not re.fullmatch(r"pangolin-m[67]-[a-z0-9-]+", project):
            raise ValueError("PostgreSQL client fallback requires the isolated M6/M7 project")
        os.environ["PANGOLIN_M6_PG_MODE"] = "container"
        os.environ["PANGOLIN_M6_PG_CONTAINER_ARGV"] = json.dumps([
            "docker", "compose", "-p", project, "-f", str(ROOT / "deploy/compose/m1.yaml"),
            "exec", "-T", "postgres"])


def validate_mapping(mapping):
    groups = m0.load(ROOT / "tests/contract/subtests.json")["groups"]
    expected = {subtest["id"] for group in groups if group["milestone"] == "M6" for subtest in group["subtests"]}
    sets = [set(mapping[key]) for key in FIELDS]
    if mapping["milestone"] != "M6" or mapping["baseline"] != "v1.3" or set.union(*sets) != expected or sum(map(len, sets)) != len(expected):
        raise ValueError("M6 acceptance mapping incomplete or duplicated")
    if mapping.get("pending") or any(not test for key in FIELDS for test in mapping[key].values()):
        raise ValueError("M6 has unimplemented acceptance")
    drill_ids = {subtest["id"] for group in groups if group["milestone"] == "M6" for subtest in group["subtests"] if subtest["id"].startswith("A72/")}
    if set(mapping["drills"]) != drill_ids or sorted(mapping["drills"].values()) != sorted(release_manifest.DRILLS):
        raise ValueError("M6 drills must map every A72 subtest to its executable drill")
    field = mapping.get("regression_integration_tests", {})
    if any(not name for name in (field.values() if isinstance(field, dict) else field)):
        raise ValueError("M6 regression integration mapping incomplete")


def drills_project(project):
    """The drills script reuses the running pangolin-m6-*/pangolin-m7-* service
    project for its docker compose pg-client fallback and never starts or stops
    services."""
    if not re.fullmatch(r"pangolin-m[67]-[a-z0-9-]+", project):
        raise ValueError("M6 drills require the dedicated pangolin-m6-* or pangolin-m7-* service project: " + project)
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
    data = m0.load(report_path)
    platform = report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"]
    release_manifest.validate_drills(data, source, report["commit"], report["source_sha256"], platform, project=data.get("project"), host_run=True)
    target.mkdir(parents=True, exist_ok=True)
    for path in sorted(path for path in source.rglob("*") if path.is_file()):
        destination = target / path.relative_to(source)
        if not destination.resolve().is_relative_to(target.resolve()):
            raise ValueError("drill artifact escapes its directory: " + str(path))
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, destination)
    copied = m0.load(target / "release-drills.json")
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
        # scripts/m6_drills.py detects a dirty tree itself and labels the
        # report development-only; only clean commits produce evidence-ready
        # drill reports the gate will accept.
        execute([sys.executable, "scripts/m6_drills.py", "--output", str(drills), "--project", project], "release-drills")
        report["release_drills"] = {"host_run": False, "project": project}
    report["inputs"]["deploy/release/manifest.json"] = m0.digest(ROOT / "deploy/release/manifest.json")
    if os.getenv("PANGOLIN_M6_RUNNER_IMAGE_ID"):
        report["runner_image"] = {"name": os.environ["PANGOLIN_M6_RUNNER_IMAGE"],
                                  "id": os.environ["PANGOLIN_M6_RUNNER_IMAGE_ID"],
                                  "dockerfile_sha256": m0.digest(ROOT / "deploy/docker/Dockerfile.m6-runner")}
    evidence = release_manifest.build_evidence(report, output)
    # The cumulative runner will still recheck the frozen source and finalize
    # status after this hook; a failed final report cannot clear the Gate.
    index = m6_index.write_index({**report, "status": "passed"}, output)
    report["profile_evidence"] = {"path": m6_index.EVIDENCE_FILE, "totals": index["totals"]}
    report["release"] = {"status": evidence["status"], "product_version": evidence["product_version"],
                          "drills": [entry["name"] for entry in evidence["drills"]["entries"]],
                          "critical_packages": [entry["package"] for entry in evidence["coverage"]["critical"]],
                          "drills_provenance": evidence["drills"]["provenance"]}


def validate_report(report, commit, directory, mapping):
    directory = Path(directory)
    m5.validate_report(report, commit, directory, m0.load(ROOT / "tests/contract/m5.json"), milestone="M6")
    commands = {command["name"] for command in report["commands"]}
    required_commands = {"m6-regression", "release-manifest"}
    if not report.get("release_drills", {}).get("host_run"):
        required_commands.add("release-drills")
    if not required_commands <= commands:
        raise ValueError("M6 release acceptance commands missing")
    if report["inputs"].get("tests/contract/m6.json") != m0.digest(ROOT / "tests/contract/m6.json"):
        raise ValueError("M6 acceptance mapping changed")
    if report["inputs"].get("deploy/release/manifest.json") != m0.digest(ROOT / "deploy/release/manifest.json"):
        raise ValueError("release manifest changed after verification")
    artifacts = {name.replace("\\", "/"): digest for name, digest in report["artifacts"].items()}
    for name in ("release-manifest.json", "release-drills/release-drills.json", "coverage.json", "templates.json", m6_index.EVIDENCE_FILE):
        if name not in artifacts:
            raise ValueError("M6 raw release evidence missing: " + name)
    evidence = m0.load(directory / "release-manifest.json")
    release_manifest.validate_evidence(evidence, report, commit, directory)
    if m0.load(directory / m6_index.EVIDENCE_FILE) != m6_index.build_index(report, directory):
        raise ValueError("per-profile evidence index cannot be reproduced from the raw artifacts")
    if {entry["name"] for entry in evidence["drills"]["entries"]} != set(mapping["drills"].values()):
        raise ValueError("M6 drill coverage incomplete")
    if evidence["regressions"].get("m6-regression") != len(mapping["python_tests"]):
        raise ValueError("M6 python acceptance evidence incomplete")


def gate(commit, mapping):
    root = ROOT / "artifacts" / commit / "m6"
    reports = {}
    for label in ("windows-amd64", "linux-amd64"):
        directory = root / label
        report = m0.load(directory / "report.json")
        validate_report(report, commit, directory, mapping)
        if report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"] != label:
            raise ValueError("wrong evidence platform")
        reports[label] = report
    if reports["windows-amd64"]["source_sha256"] != reports["linux-amd64"]["source_sha256"]:
        raise ValueError("different source trees")
    evidence = {label: m0.load(root / label / "release-manifest.json") for label in reports}
    for label, report in reports.items():
        if evidence[label]["commit"] != commit or evidence[label]["source_sha256"] != report["source_sha256"]:
            raise ValueError("release evidence is not bound to the verified source: " + label)
    for key in ("status", "baseline", "product_version", "version_scope", "versions", "event_schemas",
                "requirements", "subtests", "deliveries", "profiles", "closure", "regressions",
                "template_bundle", "template_profiles", "manifest_sha256", "gates"):
        if evidence["windows-amd64"][key] != evidence["linux-amd64"][key]:
            raise ValueError("release declarations differ across platforms: " + key)
    result = {"milestone": "M6", "status": "verified", "source_commit": commit,
              "source_sha256": reports["windows-amd64"]["source_sha256"], "baseline": "v1.3",
              "named_subtests": sum(len(mapping[key]) for key in FIELDS),
              "total_named_subtests": evidence["windows-amd64"]["subtests"],
              "requirements": evidence["windows-amd64"]["requirements"],
              "reports": {label: m0.digest(root / label / "report.json") for label in reports},
              "profile_evidence": {label: m0.digest(root / label / m6_index.EVIDENCE_FILE) for label in reports},
              "release_manifest": {label: m0.digest(root / label / "release-manifest.json") for label in reports}}
    (root / "gate.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["verify", "gate"])
    parser.add_argument("--project")
    parser.add_argument("--commit")
    parser.add_argument("--race", action="store_true")
    parser.add_argument("--external-broker-fixture", action="store_true")
    args = parser.parse_args()
    mapping = m0.load(ROOT / "tests/contract/m6.json")
    validate_mapping(mapping)
    if args.action == "gate":
        if not args.commit or not re.fullmatch(r"[0-9a-f]{40}", args.commit):
            parser.error("gate requires full --commit")
        gate(args.commit, mapping)
        return 0
    if not args.project:
        parser.error("verify requires --project")
    os.environ.update(m5.database_env())
    prepare_pg_clients(args.project)
    if args.external_broker_fixture:
        if not os.getenv("PANGOLIN_M3_BROKER_RESTART_DIR"):
            parser.error("external broker fixture requires barrier directory")
        return m1.verify(args, increment=mapping, extra_steps=release_steps)
    with m3_fixture.broker_restart(args.project, ROOT / "artifacts" / "m6-broker-barrier", milestone=6) as directory:
        os.environ["PANGOLIN_M3_BROKER_RESTART_DIR"] = str(directory)
        return m1.verify(args, increment=mapping, extra_steps=release_steps)


if __name__ == "__main__":
    raise SystemExit(main())
