"""Specification 13.6 per-profile evidence index.

build_index(report, directory) turns one same-commit verification report into
the release evidence index demanded by specification 13.6: every one of the
532 named subtests (431 legacy plus the 2026-11-25 supplemental root-DI
families), one row per release profile, with the applicability reason
taken verbatim from the planned profile manifests and the actual runtime
evidence bound to the exact raw artifact (path, sha256, test identity).  The
index never invents success flags: a required row is only written after the
mapped case is proven against the raw JSONL/Python/template/drill artifact,
and not-applicable rows keep their reason without any pass claim.  The planned
profile manifests are read-only inputs; write_index stores profile-evidence.json
in the report directory so the lead's final artifact hashing covers it.
"""
import json
from pathlib import Path, PurePosixPath
import re

import m0
import m1
import release_manifest

ROOT = m0.ROOT
BASELINE = "v1.3"
NAMED_SUBTESTS = release_manifest.NAMED_SUBTESTS
TEMPLATE_PROFILES = ("tenant-only", "organization-enabled")
TEMPLATE_CONTRACT = "both-profiles-build-test-start"
EVIDENCE_FILE = "profile-evidence.json"


def _mappings(root):
    root = Path(root)
    return {number: m0.load(root / f"tests/contract/m{number}.json") for number in range(1, 7)}


def _subtest_groups(root, max_milestone=6):
    """Groups within the requested milestone scope (the M6 index is M1-M6)."""
    groups = m0.load(Path(root) / "tests/contract/subtests.json")["groups"]
    return [group for group in groups
            if isinstance(group.get("milestone"), str) and group["milestone"][1:].isdigit()
            and int(group["milestone"][1:]) <= max_milestone]

def _profiles(root, groups):
    root = Path(root)
    names = list(release_manifest.PROFILES)
    manifests = [m0.load(root / f"tests/contract/profiles/{name}.json") for name in names]
    m0.check_profiles(groups, manifests, max_milestone=6)
    return list(zip(names, manifests))


def _fields(mapping):
    """Exactly the fields the release gate accepts per milestone so the index
    and the manifest closure can never disagree about what is mapped."""
    fields = ["unit_tests", "integration_tests", "python_tests"]
    if mapping.get("milestone") == "M1":
        fields = ["go_tests", "python_tests"]
    else:
        fields.append("template_tests")
    if mapping.get("milestone") == "M6":
        fields.append("drills")
    return fields


def _classify(milestone, field, identity):
    if field in ("go_tests", "unit_tests"):
        return "go-test", "unit"
    if field == "integration_tests":
        return "integration-test", "integration"
    if field == "python_tests":
        suffix = milestone[1:]
        if not suffix.isdigit():
            raise ValueError("mapping milestone is not a named case set: " + str(milestone))
        return "python-regression", "m" + suffix.lower() + "-regression"
    if field == "template_tests":
        if identity != TEMPLATE_CONTRACT:
            raise ValueError("the only accepted template contract is " + TEMPLATE_CONTRACT + ", got: " + identity)
        return "template", "templates"
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
    commands = [command for command in report.get("commands") or []
                if isinstance(command, dict) and command.get("name") == command_name]
    if len(commands) != 1 or commands[0].get("exit_code") != 0:
        raise ValueError("the " + command_name + " command evidence is missing, duplicated or failed for " + identity)
    command = commands[0]
    path, key = _artifact_path(directory, command.get("log"))
    if not path.is_file():
        raise ValueError("the " + command_name + " log is missing: " + key)
    digest = m0.digest(path)
    if command.get("sha256") != digest:
        raise ValueError("the " + command_name + " log changed against the recorded command hash: " + key)
    if artifacts.get(key) not in (None, digest):
        raise ValueError("the " + command_name + " log changed against the report artifact hash: " + key)
    return path, key, digest


def _log_proof(report, directory, artifacts, kind, command_name, identity):
    path, key, digest = _bound_log(report, directory, artifacts, command_name, identity)
    m1.validate_tests(path.read_text(encoding="utf-8-sig"), (identity,))
    return {"kind": kind, "command": command_name, "exit_code": 0, "log": key, "sha256": digest, "identity": identity}


def _python_proof(report, directory, artifacts, command_name, identity):
    path, key, digest = _bound_log(report, directory, artifacts, command_name, identity)
    m1.validate_python_output(path.read_text(encoding="utf-8-sig"), (identity,))
    return {"kind": "python-regression", "command": command_name, "exit_code": 0,
            "log": key, "sha256": digest, "identity": identity}


def _template_proof(report, directory, artifacts, command_name, contract):
    _bound_log(report, directory, artifacts, command_name, contract)
    path, key = _artifact_path(directory, "templates.json")
    if not path.is_file():
        raise ValueError("template evidence is missing: " + key)
    digest = m0.digest(path)
    if artifacts.get(key) not in (None, digest):
        raise ValueError("template evidence changed against the report artifact hash: " + key)
    data = m0.load(path)
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
    return {"kind": "template", "contract": contract, "command": command_name,
            "artifact": key, "sha256": digest, "build_test_start_profiles": sorted(TEMPLATE_PROFILES)}


def _drill_proof(report, directory, artifacts, drill_name):
    path, key = _artifact_path(directory, "release-drills/release-drills.json")
    if not path.is_file():
        raise ValueError("drill report is missing: " + key)
    digest = m0.digest(path)
    if artifacts.get(key) not in (None, digest):
        raise ValueError("drill report changed against the report artifact hash: " + key)
    data = m0.load(path)
    entries = data.get("drills") if isinstance(data, dict) else None
    entry = entries.get(drill_name) if isinstance(entries, dict) else None
    if not isinstance(entry, dict) or entry.get("status") != "passed":
        raise ValueError("release drill was not passed: " + drill_name)
    if data.get("commit") != report.get("commit") or data.get("source_sha256") != report.get("source_sha256"):
        raise ValueError("drill provenance does not match the index commit/source bytes")
    return {"kind": "drill", "entry": drill_name, "entry_status": "passed", "artifact": key,
            "sha256": digest, "provenance": {"commit": data["commit"], "source_sha256": data["source_sha256"]}}


def _evidence_class(report):
    """Development reports stay inspectable but can never be labelled Verified."""
    if report.get("status") == "failed" or report.get("working_tree_dirty") is True or report.get("gate") == "development-only":
        return "development-only"
    if report.get("gate") == "evidence-ready":
        return "evidence-ready"
    platform = report.get("platform")
    raced = any(isinstance(command, dict) and command.get("name") == "race" and command.get("exit_code") == 0
                for command in report.get("commands") or [])
    if isinstance(platform, dict) and platform.get("GOOS") == "linux" and not raced:
        return "development-only"
    return "evidence-ready"


def build_index(report, directory, root=ROOT):
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
    label = platform["GOOS"] + "-" + platform["GOARCH"] \
        if isinstance(platform, dict) and isinstance(platform.get("GOOS"), str) and isinstance(platform.get("GOARCH"), str) else None
    artifacts = {_norm(name): digest for name, digest in (report.get("artifacts") or {}).items()}
    groups = _subtest_groups(root)
    mappings = _mappings(root)
    closure = release_manifest.check_closure(mappings, groups)
    if closure["mapped"] != NAMED_SUBTESTS:
        raise ValueError("specification 13.6 requires exactly " + str(NAMED_SUBTESTS) +
                         " unique named subtests, found " + str(closure["mapped"]))
    cases = {}
    for number in sorted(mappings):
        mapping = mappings[number]
        for field in _fields(mapping):
            for subtest, identity in (mapping.get(field) or {}).items():
                if not isinstance(subtest, str) or not subtest or not isinstance(identity, str) or not identity:
                    raise ValueError("empty acceptance mapping entry: " + str(subtest))
                kind, command = _classify(mapping.get("milestone"), field, identity)
                if subtest in cases:
                    raise ValueError("duplicate mapped subtest: " + subtest)
                cases[subtest] = (field, identity, kind, command)
    proofs = {}
    for subtest, (_, identity, kind, command) in cases.items():
        if kind in ("go-test", "integration-test"):
            proofs[subtest] = _log_proof(report, directory, artifacts, kind, command, identity)
        elif kind == "python-regression":
            proofs[subtest] = _python_proof(report, directory, artifacts, command, identity)
        elif kind == "template":
            proofs[subtest] = _template_proof(report, directory, artifacts, command, identity)
        else:
            proofs[subtest] = _drill_proof(report, directory, artifacts, identity)
    requirement_of, milestone_of = {}, {}
    for group in groups:
        for subtest in group["subtests"]:
            requirement_of[subtest["id"]] = group.get("requirement", "")
            milestone_of[subtest["id"]] = group["milestone"]
    applicable = {}
    profile_digests = {}
    for name, manifest in _profiles(root, groups):
        profile_digests[name] = m0.digest(root / "tests/contract/profiles" / (name + ".json"))
        rows_by_id = {}
        for entry in manifest.get("subtests") or []:
            ident = entry.get("id")
            if ident not in requirement_of:
                # Later additive milestones may extend the profile manifests;
                # this per-profile index covers exactly its own milestone scope.
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
    profile_summary = {name: {"manifest_sha256": profile_digests[name],
                              "required": sum(1 for entry in applicable[name].values() if entry["applicability"] == "required"),
                              "not_applicable": sum(1 for entry in applicable[name].values() if entry["applicability"] != "required")}
                       for name in profile_order}
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
                profile_rows[name] = {"applicability": "required", "reason": entry["reason"],
                                      "evidence": {**proofs[subtest], **mark}}
            else:
                profile_rows[name] = {"applicability": "not-applicable", "reason": entry["reason"], "evidence": None}
        rows.append({"subtest": subtest, "requirement": requirement_of[subtest], "milestone": milestone_of[subtest],
                     "mapping": {"field": field, "identity": identity}, "profiles": profile_rows})
    row_total = len(rows) * len(profile_order)
    contract_inputs = {"tests/contract/" + f"m{number}.json": m0.digest(root / f"tests/contract/m{number}.json")
                       for number in sorted(mappings)}
    contract_inputs["tests/contract/subtests.json"] = m0.digest(root / "tests/contract/subtests.json")
    return {"spec": "13.6", "kind": "release-evidence-index", "baseline": BASELINE,
            "commit": commit, "source_sha256": source, "evidence_class": _evidence_class(report),
            "report_status": status, "platform": label, "closure": closure,
            "totals": {"subtests": len(rows), "rows": row_total, "required_rows": required_rows,
                       "not_applicable_rows": row_total - required_rows},
            "profiles": profile_summary, "contract_inputs": contract_inputs, "rows": rows}


def write_index(report, directory, root=ROOT):
    index = build_index(report, directory, root)
    Path(directory).mkdir(parents=True, exist_ok=True)
    (Path(directory) / EVIDENCE_FILE).write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return index
