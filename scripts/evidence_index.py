"""Specification 13.6 per-profile evidence index for release and realtime scopes.

build_index(report, directory, scope="release"|"realtime") turns one same-commit
verification report into the per-profile evidence index demanded by specification
13.6:
- scope="release": 95 requirements / 532 named subtests (M1-M6), written to
  profile-evidence.json.
- scope="realtime": 104 requirements / 593 named subtests (M1-M7, adding
  RT-001..RT-009 and the official @microsoft/signalr 10.0.0 client harness),
  written to profile-evidence-m7.json.

Every required row is bound to the exact raw artifact (path, sha256, test
identity), and not-applicable rows preserve their applicability reason without
any pass claim. The planned profile manifests are read-only inputs.
"""
import json
from pathlib import Path, PurePosixPath
import re

import contracts
import release_manifest
import test_runner

ROOT = contracts.ROOT
BASELINE = "v1.3"
BASELINE_REQUIREMENTS = 95
REALTIME_REQUIREMENTS = 9
BASELINE_SUBTESTS = release_manifest.NAMED_SUBTESTS
REALTIME_SUBTESTS = 593
REALTIME_NAMED = 61
TEMPLATE_PROFILES = ("tenant-only", "organization-enabled")
OFFICIAL_PROFILES = ("tenant-only", "organization-enabled", "replacement-contract")
TEMPLATE_CONTRACT = "both-profiles-build-test-start"
REALTIME_CONTRACT = "examples-realtime-official-client"
GATE_CONTRACT = "m7-cumulative-gates"
EVIDENCE_FILE = "profile-evidence.json"
REALTIME_EVIDENCE_FILE = "profile-evidence-m7.json"
RELEASE_EVIDENCE_FILE = "release-m7.json"
RELEASE_E2E_FILE = "realtime-e2e.json"
FIELDS = ("unit_tests", "integration_tests", "python_tests", "template_tests")


def _scope_config(scope):
    if scope == "release":
        return {"max_milestone": 6, "subtests": BASELINE_SUBTESTS, "evidence_file": EVIDENCE_FILE, "milestone_scope": None}
    if scope == "realtime":
        return {"max_milestone": 7, "subtests": REALTIME_SUBTESTS, "evidence_file": REALTIME_EVIDENCE_FILE, "milestone_scope": "M7"}
    raise ValueError("unknown evidence index scope: " + str(scope))


def _mappings(root, max_milestone=6):
    root = Path(root)
    return {number: contracts.load(root / f"tests/contract/m{number}.json") for number in range(1, max_milestone + 1)}


def _subtest_groups(root, max_milestone=6):
    """Groups within the requested milestone scope."""
    groups = contracts.load(Path(root) / "tests/contract/subtests.json")["groups"]
    return [
        group for group in groups
        if isinstance(group.get("milestone"), str) and group["milestone"][1:].isdigit()
        and int(group["milestone"][1:]) <= max_milestone
    ]


def _profiles(root, groups, max_milestone=6):
    root = Path(root)
    names = list(release_manifest.PROFILES)
    manifests = [contracts.load(root / f"tests/contract/profiles/{name}.json") for name in names]
    contracts.check_profiles(groups, manifests, max_milestone=max_milestone)
    return list(zip(names, manifests))


def _classify(milestone, field, identity, scope="release"):
    """Classify an acceptance row into its proof kind and bound command."""
    if field in ("go_tests", "unit_tests"):
        return "go-test", "unit"
    if field == "integration_tests":
        return "integration-test", "integration"
    if field == "python_tests":
        suffix = str(milestone)[1:]
        if not suffix.isdigit():
            raise ValueError("mapping milestone is not a named case set: " + str(milestone))
        return "python-regression", "m" + suffix.lower() + "-regression"
    if field == "template_tests":
        if scope == "realtime" and identity == REALTIME_CONTRACT:
            return "realtime", "realtime-e2e"
        if identity == TEMPLATE_CONTRACT:
            return "template", "templates"
        if scope == "realtime":
            raise ValueError("unknown realtime template contract: " + str(identity))
        raise ValueError("the only accepted template contract is " + TEMPLATE_CONTRACT + ", got: " + str(identity))
    if field == "gate_checks":
        if scope != "realtime" or identity != GATE_CONTRACT:
            raise ValueError("the only accepted cumulative-gate contract is " + GATE_CONTRACT + ", got: " + str(identity))
        return "cumulative-gates", GATE_CONTRACT
    if field == "drills":
        if identity not in release_manifest.DRILLS:
            raise ValueError("unknown release drill: " + str(identity))
        return "drill", release_manifest.DRILLS
    raise ValueError("unmappable acceptance field: " + field)


def _norm(relative):
    if not isinstance(relative, str) or not relative:
        raise ValueError("evidence path is empty or is not a string")
    key = relative.replace("\\", "/")
    pure = PurePosixPath(key)
    if ":" in key or pure.is_absolute() or ".." in pure.parts:
        raise ValueError("evidence path escapes the report directory: " + key)
    return key


def _artifact_path(directory, relative):
    key = _norm(relative)
    path = Path(directory)
    for part in PurePosixPath(key).parts:
        path = path / part
    path = path.resolve()
    if not path.is_relative_to(Path(directory).resolve()):
        raise ValueError("evidence path escapes the report directory: " + key)
    return path, key


def _bound_log(report, directory, artifacts, command_name, identity):
    commands = [
        command for command in report.get("commands") or []
        if isinstance(command, dict) and command.get("name") == command_name
    ]
    if len(commands) != 1 or commands[0].get("exit_code") != 0:
        raise ValueError("the " + command_name + " command evidence is missing, duplicated or failed for " + identity)
    command = commands[0]
    path, key = _artifact_path(directory, command.get("log"))
    if not path.is_file():
        raise ValueError("the " + command_name + " log is missing: " + key)
    digest = contracts.digest(path)
    if command.get("sha256") != digest:
        raise ValueError("the " + command_name + " log changed against the recorded command hash: " + key)
    if artifacts.get(key) not in (None, digest):
        raise ValueError("the " + command_name + " log changed against the report artifact hash: " + key)
    return path, key, digest


def _log_proof(report, directory, artifacts, kind, command_name, identity):
    path, key, digest = _bound_log(report, directory, artifacts, command_name, identity)
    test_runner.validate_tests(path.read_text(encoding="utf-8-sig"), (identity,))
    return {"kind": kind, "command": command_name, "exit_code": 0, "log": key, "sha256": digest, "identity": identity}


def _python_proof(report, directory, artifacts, command_name, identity):
    path, key, digest = _bound_log(report, directory, artifacts, command_name, identity)
    test_runner.validate_python_output(path.read_text(encoding="utf-8-sig"), (identity,))
    return {"kind": "python-regression", "command": command_name, "exit_code": 0, "log": key, "sha256": digest, "identity": identity}


def _template_proof(report, directory, artifacts, command_name, contract):
    _bound_log(report, directory, artifacts, command_name, contract)
    path, key = _artifact_path(directory, "templates.json")
    if not path.is_file():
        raise ValueError("template evidence is missing: " + key)
    digest = contracts.digest(path)
    if artifacts.get(key) not in (None, digest):
        raise ValueError("template evidence changed against the report artifact hash: " + key)
    data = contracts.load(path)
    if not isinstance(data, dict) or data.get("status") != "passed":
        raise ValueError("template evidence does not record a passed status")
    rows = {}
    for entry in data.get("profiles") or []:
        if not isinstance(entry, dict) or not isinstance(entry.get("profile"), str) or not entry["profile"].strip():
            raise ValueError("template evidence records an unnamed profile row")
        if entry["profile"] in rows:
            raise ValueError("template evidence records duplicate profile rows: " + entry["profile"])
        rows[entry["profile"]] = entry
    for name in TEMPLATE_PROFILES:
        if not isinstance(rows.get(name), dict) or rows[name].get("build_test_start") is not True:
            raise ValueError("template evidence does not prove build/test/start for " + name)
    return {
        "kind": "template",
        "contract": contract,
        "command": command_name,
        "artifact": key,
        "sha256": digest,
        "build_test_start_profiles": sorted(TEMPLATE_PROFILES),
    }


def _drill_proof(report, directory, artifacts, drill_name):
    path, key = _artifact_path(directory, "release-drills/release-drills.json")
    if not path.is_file():
        raise ValueError("drill report is missing: " + key)
    digest = contracts.digest(path)
    if artifacts.get(key) not in (None, digest):
        raise ValueError("drill report changed against the report artifact hash: " + key)
    data = contracts.load(path)
    entries = data.get("drills") if isinstance(data, dict) else None
    entry = entries.get(drill_name) if isinstance(entries, dict) else None
    if not isinstance(entry, dict) or entry.get("status") != "passed":
        raise ValueError("release drill was not passed: " + drill_name)
    if data.get("commit") != report.get("commit") or data.get("source_sha256") != report.get("source_sha256"):
        raise ValueError("drill provenance does not match the index commit/source bytes")
    return {
        "kind": "drill",
        "entry": drill_name,
        "entry_status": "passed",
        "artifact": key,
        "sha256": digest,
        "provenance": {"commit": data["commit"], "source_sha256": data["source_sha256"]},
    }


def parse_official_log(text):
    """Top-level TAP results of the official-client integration harness."""
    lines = text.splitlines()
    if any(line.startswith("not ok") for line in lines):
        raise ValueError("official-client harness recorded a failed test")
    top_level = [line for line in lines if re.match(r"^ok \d+ - ", line)]
    if any(re.search(r"#\s+(skip|SKIP)\b", line) for line in top_level):
        raise ValueError("official-client harness recorded a skipped test")
    names = [re.match(r"^ok \d+ - (.+)$", line).group(1).removesuffix(" # SKIP").removesuffix(" # skip") for line in top_level]
    if not names:
        raise ValueError("official-client harness produced no passing top-level tests")
    summary = {}
    for key, match in ((key, re.search(r"^# " + key + r" (\d+)$", text, re.M)) for key in ("tests", "suites", "pass", "fail", "cancelled", "skipped", "todo")):
        if match:
            summary[key] = int(match.group(1))
    missing = [key for key in ("tests", "pass", "fail", "cancelled", "skipped") if key not in summary]
    if missing:
        raise ValueError("official-client TAP summary is incomplete, missing " + ", ".join(missing))
    if summary["fail"] != 0:
        raise ValueError("official-client harness recorded failed tests")
    if summary["cancelled"] != 0:
        raise ValueError("official-client harness recorded cancelled tests")
    if summary["skipped"] != 0:
        raise ValueError("official-client harness recorded skipped tests")
    if summary["tests"] != len(names) or summary["pass"] != len(names):
        raise ValueError("official-client TAP summary contradicts the top-level results: " + str(summary) + " for " + str(len(names)) + " passing top-level tests")
    profiles = {}
    for name in names:
        profile = next((candidate for candidate in OFFICIAL_PROFILES if name.startswith(candidate)), None)
        if profile is None:
            raise ValueError("official-client harness ran an unknown top-level test: " + name)
        profiles.setdefault(profile, []).append(name)
    duplicates = sorted(name for name, runs in profiles.items() if len(runs) > 1)
    if duplicates:
        raise ValueError("official-client harness ran duplicate profile tests: " + ", ".join(duplicates))
    absent = sorted(set(OFFICIAL_PROFILES) - set(profiles))
    if absent:
        raise ValueError("official-client harness did not run every release profile: " + ", ".join(absent))
    return {"profiles": sorted(profiles), "tests": len(names)}


def read_release_e2e(directory, root=ROOT):
    """Read and re-validate the machine-written harness evidence artifact."""
    data = contracts.load(Path(directory) / RELEASE_E2E_FILE)
    if not isinstance(data, dict) or data.get("status") != "passed":
        raise ValueError("official-client harness evidence does not record a passed status")
    if tuple(sorted(data.get("profiles") or ())) != tuple(sorted(OFFICIAL_PROFILES)):
        raise ValueError("official-client harness evidence does not cover every release profile")
    if data.get("skipped") != 0 or data.get("failed") != 0:
        raise ValueError("official-client harness evidence records skipped or failed tests")
    if not isinstance(data.get("tests"), int) or data["tests"] < len(OFFICIAL_PROFILES):
        raise ValueError("official-client harness evidence records no test count")
    return data


def _gates_proof(report, directory, artifacts):
    """Cumulative-gate proof for A104/cumulative-gates."""
    directory = Path(directory)
    commit, source = report.get("commit"), report.get("source_sha256")
    if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-f]{40}", commit) or not isinstance(source, str) or not re.fullmatch(r"[0-9a-f]{64}", source):
        raise ValueError("the cumulative-gate proof requires a full source commit and source bytes")
    if report.get("baseline") != BASELINE:
        raise ValueError("the cumulative-gate proof requires baseline " + BASELINE)
    platform = report.get("platform") or {}
    linux = platform.get("GOOS") == "linux"
    windows = platform.get("GOOS") == "windows"
    commands = [command for command in report.get("commands") or [] if isinstance(command, dict)]
    names = [command.get("name") for command in commands]
    if any(command.get("exit_code") != 0 for command in commands):
        raise ValueError("the cumulative-gate proof found a failed command")
    required = {
        "inventory", "build", "vet", "go-module-verify", "unit", "integration", "e2e", "coverage",
        "coverage-html", "generation", "generation-version",
        "m0-regression", "m1-regression", "m2-regression", "m3-regression", "m4-regression",
        "m5-regression", "m6-regression", "m7-regression",
        "templates", "template-source", "cli-build", "cli-coverage", "release-manifest",
        "node-version", "npm-version", "realtime-dbtool", "realtime-db-create", "realtime-demo",
        "realtime-provision", "realtime-broker-cleanup", "realtime-client-deps",
        "realtime-client-licenses", "realtime-e2e", "realtime-coverage",
        "realtime-db-drop",
    }
    if (report.get("release_drills") or {}).get("host_run") is not True:
        required.add("release-drills")
    if linux:
        required.add("race")
    if windows:
        required.add("templates-compose")
    duplicates = sorted(name for name in required if names.count(name) > 1)
    if duplicates:
        raise ValueError("the cumulative-gate proof found duplicated commands: " + ", ".join(duplicates))
    missing = sorted(required - set(names))
    if missing:
        raise ValueError("the cumulative-gate proof is missing commands: " + ", ".join(missing))
    if len([name for name in names if name == "e2e-step"]) < 3 or "e2e" not in names:
        raise ValueError("the cumulative-gate proof is missing the recorded E2E process evidence")
    artifact_names = ("coverage.json", "merged.out", "go-list.json", "exclusions.json", "dependencies.json", "templates.json", "client-dependencies.json")
    bound = {}
    for name in artifact_names:
        digest = artifacts.get(name)
        if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
            path = directory / name
            if not path.is_file():
                raise ValueError("the cumulative-gate proof is missing a bound artifact: " + name)
            digest = contracts.digest(path)
        else:
            disk = directory / name
            if not disk.is_file() or contracts.digest(disk) != digest:
                raise ValueError("the cumulative-gate proof is missing a bound artifact: " + name)
        bound[name] = digest
    coverage = contracts.load(directory / "coverage.json")
    overall = coverage.get("overall") or {}
    total, covered = int(overall.get("total_statements", 0)), int(overall.get("covered_statements", 0))
    if total <= 0 or covered > total or covered * 100 < total * 90:
        raise ValueError("the cumulative-gate proof found overall raw statement coverage below 90%")
    for name in (("integration", "race") if linux else ("integration", "coverage")):
        _bound_log(report, directory, artifacts, name, "cumulative-gates")
    _bound_log(report, directory, artifacts, "release-manifest", "cumulative-gates")
    return {
        "kind": "cumulative-gates",
        "commit": commit,
        "source_sha256": source,
        "commands": sorted(required),
        "linux_race": linux,
        "coverage": {"total_statements": total, "covered_statements": covered},
        "artifacts": bound,
    }


def _realtime_proof(report, directory, artifacts):
    path, key, digest = _bound_log(report, directory, artifacts, "realtime-e2e", REALTIME_CONTRACT)
    parsed = parse_official_log(path.read_text(encoding="utf-8-sig"))
    data = read_release_e2e(directory)
    artifact, artifact_key = _artifact_path(directory, RELEASE_E2E_FILE)
    if not artifact.is_file():
        raise ValueError("official-client harness evidence is missing: " + artifact_key)
    artifact_digest = contracts.digest(artifact)
    if artifacts.get(artifact_key) not in (None, artifact_digest):
        raise ValueError("official-client harness evidence changed against the report artifact hash")
    if parsed["profiles"] != sorted(data["profiles"]):
        raise ValueError("official-client harness log and artifact disagree about the profiles")
    return {
        "kind": "realtime-e2e",
        "command": "realtime-e2e",
        "exit_code": 0,
        "log": key,
        "sha256": digest,
        "artifact": artifact_key,
        "artifact_sha256": artifact_digest,
        "profiles": parsed["profiles"],
        "tests": parsed["tests"],
    }


def _evidence_class(report):
    """Development reports stay inspectable but can never be labelled Verified."""
    if report.get("status") == "failed" or report.get("working_tree_dirty") is True or report.get("gate") == "development-only":
        return "development-only"
    if report.get("gate") == "evidence-ready":
        return "evidence-ready"
    platform = report.get("platform")
    raced = any(isinstance(command, dict) and command.get("name") == "race" and command.get("exit_code") == 0 for command in report.get("commands") or [])
    if isinstance(platform, dict) and platform.get("GOOS") == "linux" and not raced:
        return "development-only"
    return "evidence-ready"


def build_index(report, directory, root=ROOT, scope="release"):
    cfg = _scope_config(scope)
    max_milestone = cfg["max_milestone"]
    root, directory = Path(root), Path(directory)
    if not isinstance(report, dict):
        raise ValueError("the evidence index requires a verification report dictionary")
    commit = report.get("commit")
    if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("the evidence index requires a full 40-hex source commit")
    source = report.get("source_sha256")
    if not isinstance(source, str) or not re.fullmatch(r"[0-9a-f]{64}", source):
        raise ValueError("the evidence index requires a 64-hex source hash")
    if report.get("baseline") != BASELINE:
        raise ValueError("the evidence index requires baseline " + BASELINE)
    status = report.get("status")
    if status is not None and not isinstance(status, str):
        raise ValueError("the report status is not a string")
    platform = report.get("platform")
    label = platform["GOOS"] + "-" + platform["GOARCH"] if isinstance(platform, dict) and isinstance(platform.get("GOOS"), str) and isinstance(platform.get("GOARCH"), str) else None
    artifacts = {_norm(name): digest for name, digest in (report.get("artifacts") or {}).items()}
    groups = _subtest_groups(root, max_milestone=max_milestone)
    mappings = _mappings(root, max_milestone=max_milestone)
    closure = release_manifest.check_closure(mappings, groups)
    if scope == "release":
        if closure["mapped"] != cfg["subtests"]:
            raise ValueError("specification 13.6 requires exactly " + str(cfg["subtests"]) + " unique named subtests, found " + str(closure["mapped"]))
    else:
        if closure["mapped"] != cfg["subtests"] or closure["milestones"].get("M7") != REALTIME_NAMED:
            raise ValueError("specification 13.6 requires exactly " + str(cfg["subtests"]) + " unique named subtests mapped across M1..M7, found " + str(closure["mapped"]))
    cases = {}
    for number in sorted(mappings):
        mapping = mappings[number]
        for field in release_manifest._milestone_fields(mapping):
            for subtest, identity in (mapping.get(field) or {}).items():
                if not isinstance(subtest, str) or not subtest or not isinstance(identity, str) or not identity:
                    raise ValueError("empty acceptance mapping entry: " + str(subtest))
                kind, command = _classify(mapping.get("milestone"), field, identity, scope=scope)
                if subtest in cases:
                    raise ValueError("duplicate mapped subtest: " + subtest)
                cases[subtest] = (field, identity, kind, command)
    proofs = {}
    for subtest, (_, identity, kind, command) in cases.items():
        if kind == "realtime":
            proofs[subtest] = _realtime_proof(report, directory, artifacts)
        elif kind == "cumulative-gates":
            proofs[subtest] = _gates_proof(report, directory, artifacts)
        elif kind == "drill":
            proofs[subtest] = _drill_proof(report, directory, artifacts, identity)
        elif kind == "python-regression":
            proofs[subtest] = _python_proof(report, directory, artifacts, command, identity)
        elif kind == "template":
            proofs[subtest] = _template_proof(report, directory, artifacts, command, identity)
        else:
            proofs[subtest] = _log_proof(report, directory, artifacts, kind, command, identity)
    requirement_of, milestone_of = {}, {}
    for group in groups:
        for subtest in group["subtests"]:
            requirement_of[subtest["id"]] = group.get("requirement", "")
            milestone_of[subtest["id"]] = group["milestone"]
    applicable, profile_digests = {}, {}
    for name, manifest in _profiles(root, groups, max_milestone=max_milestone):
        profile_digests[name] = contracts.digest(root / "tests/contract/profiles" / (name + ".json"))
        rows_by_id = {}
        for entry in manifest.get("subtests") or []:
            ident = entry.get("id")
            if scope == "release" and ident not in requirement_of:
                # Later additive milestones may extend the profile manifests;
                # the release index covers exactly the release scope.
                continue
            if ident in rows_by_id:
                raise ValueError("duplicate profile row: " + ident)
            applicability = entry.get("applicability")
            if applicability not in ("required", "not-applicable"):
                raise ValueError("unknown profile applicability for " + ident + ": " + str(applicability))
            reason = entry.get("reason")
            if not isinstance(reason, str) or (applicability == "not-applicable" and not reason.strip()):
                raise ValueError("profile row reason is missing or empty for " + ident)
            rows_by_id[ident] = entry
        if set(rows_by_id) != set(cases):
            raise ValueError("profile plan does not cover exactly the named subtests: " + name)
        applicable[name] = rows_by_id
    profile_order = sorted(applicable)
    profile_summary = {
        name: {
            "manifest_sha256": profile_digests[name],
            "required": sum(1 for entry in applicable[name].values() if entry["applicability"] == "required"),
            "not_applicable": sum(1 for entry in applicable[name].values() if entry["applicability"] != "required"),
        }
        for name in profile_order
    }
    rows = []
    required_rows = 0
    for subtest in sorted(cases):
        field, identity, _, _ = cases[subtest]
        order = [name for name in profile_order if applicable[name][subtest]["applicability"] == "required"]
        required_rows += len(order)
        mark = {"shared_contract": len(order) >= 2, "linked_profiles": order}
        profile_rows = {}
        for name in profile_order:
            entry = applicable[name][subtest]
            if entry["applicability"] == "required":
                profile_rows[name] = {"applicability": "required", "reason": entry["reason"], "evidence": {**proofs[subtest], **mark}}
            else:
                profile_rows[name] = {"applicability": "not-applicable", "reason": entry["reason"], "evidence": None}
        rows.append({"subtest": subtest, "requirement": requirement_of[subtest], "milestone": milestone_of[subtest], "mapping": {"field": field, "identity": identity}, "profiles": profile_rows})
    row_total = len(rows) * len(profile_order)
    contract_inputs = {"tests/contract/" + f"m{number}.json": contracts.digest(root / f"tests/contract/m{number}.json") for number in sorted(mappings)}
    contract_inputs["tests/contract/subtests.json"] = contracts.digest(root / "tests/contract/subtests.json")
    result = {
        "spec": "13.6",
        "kind": "release-evidence-index",
        "baseline": BASELINE,
        "commit": commit,
        "source_sha256": source,
        "evidence_class": _evidence_class(report),
        "report_status": status,
        "platform": label,
        "closure": closure,
        "totals": {"subtests": len(rows), "rows": row_total, "required_rows": required_rows, "not_applicable_rows": row_total - required_rows},
        "profiles": profile_summary,
        "contract_inputs": contract_inputs,
        "rows": rows,
    }
    if cfg["milestone_scope"] is not None:
        result["milestone_scope"] = cfg["milestone_scope"]
    return result


def write_index(report, directory, root=ROOT, scope="release"):
    """Emit the per-profile evidence index with canonical LF bytes."""
    cfg = _scope_config(scope)
    index = build_index(report, directory, root=root, scope=scope)
    path = Path(directory) / cfg["evidence_file"]
    payload = (json.dumps(index, indent=2, sort_keys=True) + "\n").encode("utf-8")
    if scope == "realtime" and path.is_file() and path.read_bytes() != payload:
        raise ValueError("per-profile evidence index already exists with different content: " + cfg["evidence_file"])
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(payload)
    return index
