"""Specification 13.6 per-profile evidence index for the additive M7 scope.

build_index extends the M6 evidence machinery to the full 104-requirement
contract inventory (95 historical requirements plus RT-001..RT-009): 593 named
subtests across all three release profile plans, every required row bound to
its exact raw same-source artifact. M7's template contract
(examples-realtime-official-client) is proven from the official
@microsoft/signalr 10.0.0 harness running against two independent instrumented
realtime nodes on real PostgreSQL/NATS; nothing passes on a compile check, a
skipped case, a foreign source hash or hand-written metadata.
"""
import json
from pathlib import Path
import re

import m0
import m6_index
import release_manifest

ROOT = m0.ROOT
BASELINE = "v1.3"
MILESTONE = "M7"
BASELINE_SUBTESTS = release_manifest.NAMED_SUBTESTS
BASELINE_REQUIREMENTS = 95
REALTIME_REQUIREMENTS = 9
NAMED_SUBTESTS = 593
REALTIME_NAMED = 61
EVIDENCE_FILE = "profile-evidence-m7.json"
RELEASE_EVIDENCE_FILE = "release-m7.json"
RELEASE_E2E_FILE = "realtime-e2e.json"
REALTIME_CONTRACT = "examples-realtime-official-client"
GATE_CONTRACT = "m7-cumulative-gates"
TEMPLATE_CONTRACT = m6_index.TEMPLATE_CONTRACT
OFFICIAL_PROFILES = ("tenant-only", "organization-enabled", "replacement-contract")
FIELDS = ("unit_tests", "integration_tests", "python_tests", "template_tests")


def _mappings(root):
    root = Path(root)
    return {number: m0.load(root / f"tests/contract/m{number}.json") for number in range(1, 8)}


def parse_official_log(text):
    """Top-level TAP results of the official-client integration harness.

    node --test --test-reporter=tap emits one column-0 `ok N - <name>` per
    top-level test; nested test output is indented. The complete summary
    block (tests/pass/fail/cancelled/skipped, zero fail/cancelled/skip) must
    be present and consistent with the top-level names, and exactly one
    top-level test per release profile must have run: missing, truncated,
    contradictory, duplicate-profile or unknown-test output fails closed.
    """
    lines = text.splitlines()
    if any(line.startswith("not ok") for line in lines):
        raise ValueError("official-client harness recorded a failed test")
    top_level = [line for line in lines if re.match(r"^ok \d+ - ", line)]
    if any(re.search(r"#\s+(skip|SKIP)\b", line) for line in top_level):
        raise ValueError("official-client harness recorded a skipped test")
    names = [re.match(r"^ok \d+ - (.+)$", line).group(1).removesuffix(" # SKIP").removesuffix(" # skip")
             for line in top_level]
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
        raise ValueError("official-client TAP summary contradicts the top-level results: "
                         + str(summary) + " for " + str(len(names)) + " passing top-level tests")
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
    data = m0.load(Path(directory) / RELEASE_E2E_FILE)
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
    """Cumulative-gate proof for A104/cumulative-gates.

    Binds the executed cumulative acceptance without any circular report hash
    or self-referential pass flag: every required cumulative command must be
    present exactly once with exit code 0 (platform-dependent sets included),
    the recorded source bytes must be 40/64-hex and the baseline v1.3, key raw
    artifacts (coverage union, go-list, exclusions, dependencies, templates)
    must carry 64-hex digests in the artifacts map, the raw coverage union
    must reach the release threshold overall, and representative command logs
    (coverage, integration, race, release manifest) must be re-readable from
    disk with their bound sha256. The same-source two-platform gate remains
    the final authority; a development run keeps the development-only class.
    """
    directory, artifacts = Path(directory), artifacts
    commit, source = report.get("commit"), report.get("source_sha256")
    if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-f]{40}", commit) \
            or not isinstance(source, str) or not re.fullmatch(r"[0-9a-f]{64}", source):
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
    required = {"inventory", "build", "vet", "go-module-verify", "unit", "integration", "e2e", "coverage",
                "coverage-html", "generation", "generation-version",
                "m0-regression", "m1-regression", "m2-regression", "m3-regression", "m4-regression",
                "m5-regression", "m6-regression", "m7-regression",
                "templates", "template-source", "cli-build", "cli-coverage", "release-manifest",
                "node-version", "npm-version", "realtime-dbtool", "realtime-db-create", "realtime-demo",
                "realtime-provision", "realtime-broker-cleanup", "realtime-client-deps",
                "realtime-client-licenses", "realtime-e2e", "realtime-coverage",
                "realtime-db-drop"}
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
    artifact_names = ("coverage.json", "merged.out", "go-list.json", "exclusions.json", "dependencies.json",
                      "templates.json", "client-dependencies.json")
    bound = {}
    for name in artifact_names:
        digest = artifacts.get(name)
        if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
            # Inside the injected hook the final artifact map does not exist
            # yet; the digest is then bound straight from the report file so
            # the finalized report can always be checked against it.
            path = directory / name
            if not path.is_file():
                raise ValueError("the cumulative-gate proof is missing a bound artifact: " + name)
            digest = m0.digest(path)
        else:
            disk = directory / name
            if not disk.is_file() or m0.digest(disk) != digest:
                raise ValueError("the cumulative-gate proof is missing a bound artifact: " + name)
        bound[name] = digest
    coverage = m0.load(directory / "coverage.json")
    overall = coverage.get("overall") or {}
    total, covered = int(overall.get("total_statements", 0)), int(overall.get("covered_statements", 0))
    if total <= 0 or covered > total or covered * 100 < total * 90:
        raise ValueError("the cumulative-gate proof found overall raw statement coverage below 90%")
    for name in (("integration", "race") if linux else ("integration", "coverage")):
        m6_index._bound_log(report, directory, artifacts, name, "cumulative-gates")
    m6_index._bound_log(report, directory, artifacts, "release-manifest", "cumulative-gates")
    return {"kind": "cumulative-gates", "commit": commit, "source_sha256": source,
            "commands": sorted(required), "linux_race": linux,
            "coverage": {"total_statements": total, "covered_statements": covered},
            "artifacts": bound}


def _realtime_proof(report, directory, artifacts):
    path, key, digest = m6_index._bound_log(report, directory, artifacts, "realtime-e2e", REALTIME_CONTRACT)
    parsed = parse_official_log(path.read_text(encoding="utf-8-sig"))
    data = read_release_e2e(directory)
    artifact, artifact_key = m6_index._artifact_path(directory, RELEASE_E2E_FILE)
    if not artifact.is_file():
        raise ValueError("official-client harness evidence is missing: " + artifact_key)
    artifact_digest = m0.digest(artifact)
    if artifacts.get(artifact_key) not in (None, artifact_digest):
        raise ValueError("official-client harness evidence changed against the report artifact hash")
    if parsed["profiles"] != sorted(data["profiles"]):
        raise ValueError("official-client harness log and artifact disagree about the profiles")
    return {"kind": "realtime-e2e", "command": "realtime-e2e", "exit_code": 0, "log": key,
            "sha256": digest, "artifact": artifact_key, "artifact_sha256": artifact_digest,
            "profiles": parsed["profiles"], "tests": parsed["tests"]}


def _profiles(root, groups):
    root = Path(root)
    names = list(release_manifest.PROFILES)
    manifests = [m0.load(root / f"tests/contract/profiles/{name}.json") for name in names]
    m0.check_profiles(groups, manifests, max_milestone=7)
    return list(zip(names, manifests))


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
    artifacts = {m6_index._norm(name): digest for name, digest in (report.get("artifacts") or {}).items()}
    groups = m6_index._subtest_groups(root, max_milestone=7)
    mappings = _mappings(root)
    closure = release_manifest.check_closure(mappings, groups)
    if closure["mapped"] != NAMED_SUBTESTS or closure["milestones"].get(MILESTONE) != REALTIME_NAMED:
        raise ValueError("specification 13.6 requires exactly " + str(NAMED_SUBTESTS) +
                         " unique named subtests mapped across M1.." + MILESTONE + ", found " + str(closure["mapped"]))
    cases = {}
    for number in sorted(mappings):
        mapping = mappings[number]
        for field in release_manifest._milestone_fields(mapping):
            for subtest, identity in (mapping.get(field) or {}).items():
                if not isinstance(subtest, str) or not subtest or not isinstance(identity, str) or not identity:
                    raise ValueError("empty acceptance mapping entry: " + str(subtest))
                kind, command = _classify(field, identity)
                if kind == "python-regression":
                    command = "m" + str(number) + "-regression"
                elif command is None:
                    raise ValueError("unbound proof command for " + subtest)
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
            proofs[subtest] = m6_index._drill_proof(report, directory, artifacts, identity)
        elif kind == "python-regression":
            proofs[subtest] = m6_index._python_proof(report, directory, artifacts, command, identity)
        elif kind == "template":
            proofs[subtest] = m6_index._template_proof(report, directory, artifacts, command, identity)
        else:
            proofs[subtest] = m6_index._log_proof(report, directory, artifacts, kind, command, identity)
    requirement_of, milestone_of = {}, {}
    for group in groups:
        for subtest in group["subtests"]:
            requirement_of[subtest["id"]] = group.get("requirement", "")
            milestone_of[subtest["id"]] = group["milestone"]
    applicable, profile_digests = {}, {}
    for name, manifest in _profiles(root, groups):
        profile_digests[name] = m0.digest(root / "tests/contract/profiles" / (name + ".json"))
        rows_by_id = {}
        for entry in manifest.get("subtests") or []:
            ident = entry.get("id")
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
    return {"spec": "13.6", "kind": "release-evidence-index", "milestone_scope": MILESTONE,
            "baseline": BASELINE, "commit": commit, "source_sha256": source,
            "evidence_class": m6_index._evidence_class(report),
            "report_status": status, "platform": label, "closure": closure,
            "totals": {"subtests": len(rows), "rows": row_total, "required_rows": required_rows,
                       "not_applicable_rows": row_total - required_rows},
            "profiles": profile_summary, "contract_inputs": contract_inputs, "rows": rows}


def _classify(field, identity):
    """Classify an acceptance row into its proof kind and bound command."""
    if field in ("go_tests", "unit_tests"):
        return "go-test", "unit"
    if field == "integration_tests":
        return "integration-test", "integration"
    if field == "python_tests":
        return "python-regression", None
    if field == "template_tests":
        if identity == REALTIME_CONTRACT:
            return "realtime", "realtime-e2e"
        if identity == TEMPLATE_CONTRACT:
            kind, command = m6_index._classify("M5", "template_tests", identity)
            return kind, command
        raise ValueError("unknown M7 template contract: " + str(identity))
    if field == "gate_checks":
        if identity != GATE_CONTRACT:
            raise ValueError("the only accepted cumulative-gate contract is " + GATE_CONTRACT + ", got: " + str(identity))
        return "cumulative-gates", GATE_CONTRACT
    if field == "drills":
        if identity not in release_manifest.DRILLS:
            raise ValueError("unknown release drill: " + str(identity))
        return "drill", release_manifest.DRILLS
    raise ValueError("unmappable acceptance field: " + field)


def write_index(report, directory, root=ROOT):
    """Emit the per-profile evidence index with canonical LF bytes exactly once."""
    index = build_index(report, directory, root)
    path = Path(directory) / EVIDENCE_FILE
    payload = json.dumps(index, indent=2, sort_keys=True) + "\n"
    if path.is_file() and path.read_bytes() != payload.encode("utf-8"):
        raise ValueError("per-profile evidence index already exists with different content: " + EVIDENCE_FILE)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(payload.encode("utf-8"))
    return index
