"""Deterministic M0 baseline checks and same-commit evidence collection.

M0 validates contracts, fixture plans and engineering infrastructure. It never
marks planned product/runtime acceptance cases as passed.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
MODULE = "github.com/cymondez/pangolin-framework"


def load(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def go_environment(root):
    env = os.environ.copy()
    for name, directory in (("GOMODCACHE", "go-mod"), ("GOTMPDIR", "go-tmp")):
        path = root / ".cache" / directory
        path.mkdir(parents=True, exist_ok=True)
        env[name] = str(path)
    env["GOTOOLCHAIN"] = "local"
    return env


def command(args, root=ROOT):
    return subprocess.run(args, cwd=root, env=go_environment(root), capture_output=True, text=True, encoding="utf-8", errors="replace", check=True).stdout


def json_stream(text):
    decoder, records = json.JSONDecoder(), []
    while text.strip():
        value, end = decoder.raw_decode(text.lstrip())
        records.append(value)
        text = text.lstrip()[end:]
    return records


def api_surface(root=ROOT):
    packages = command(["go", "list", "./..."], root).splitlines()
    result = {}
    for package in packages:
        if "/internal/" in package or "/tools/" in package or "/tests/" in package:
            continue
        text = command(["go", "doc", "-all", package], root).replace("\r\n", "\n")
        result[package] = {"sha256": hashlib.sha256(text.encode()).hexdigest(), "surface": text}
    return {"baseline": "v1.3", "packages": result}


def check_api(expected, actual):
    if expected != actual:
        raise ValueError("public API surface changed; review the specification/ADR and run m0.py freeze-api explicitly")


def check_profiles(groups, profiles):
    expected = {s["id"]: g for g in groups for s in g["subtests"]}
    if len(expected) != sum(len(g["subtests"]) for g in groups):
        raise ValueError("duplicate named subtest")
    for profile in profiles:
        seen = set()
        if profile["status"] != "planned" or profile["baseline"] != "v1.3":
            raise ValueError("a profile plan cannot claim runtime verification")
        for entry in profile["subtests"]:
            ident = entry["id"]
            if ident not in expected or ident in seen or entry["requirement"] != expected[ident]["requirement"]:
                raise ValueError("invalid profile/subtest mapping: " + ident)
            seen.add(ident)
            if not entry["reason"] or entry["evidence"] != next(s["evidence"] for s in expected[ident]["subtests"] if s["id"] == ident):
                raise ValueError("missing applicability reason or evidence template")
            na = entry["applicability"] == "not-applicable"
            if entry["applicability"] not in ("required", "not-applicable") or entry["status"] != ("not-applicable" if na else "planned"):
                raise ValueError("invalid profile status")
            if na and profile["profile"] != "tenant-only":
                raise ValueError("organization/replacement fixture cannot skip required contracts")
            if na and ident.split("/")[0] in {"A75", "A92", "A95"}:
                raise ValueError("required scope/downgrade negatives cannot be N/A")
            if na and ident in {"A74/missing-required-provider", "A74/duplicate-kind", "A74/unsupported-kind-no-fallback", "A77/missing-scope", "A77/pool-reuse", "A79/missing-org-scope", "A79/unavailable-provider"}:
                raise ValueError("required scope isolation negative cannot be N/A")
        if seen != set(expected):
            raise ValueError("profile omits named subtests")


def check_imports(packages):
    for package in packages:
        name = package["ImportPath"]
        role = name[len(MODULE):].split("/")
        framework = name == MODULE or name.startswith(MODULE + "/") and role[1] in {"kernel", "persistence", "security", "events", "tenancy", "platform", "observability", "transport", "modules", "application", "domain"}
        for imported in package.get("Imports", []):
            if framework and imported.startswith(MODULE + "/examples/"):
                raise ValueError("framework imports example: " + name + " -> " + imported)
            project_role = imported[len(MODULE):].split("/") if imported.startswith(MODULE + "/") else []
            concrete = imported in {"database/sql", "net/http"} or bool({"infrastructure", "transport"} & set(project_role)) or imported.startswith(("github.com/jackc/", "github.com/nats-io/", MODULE + "/persistence/postgres", MODULE + "/events/nats", MODULE + "/transport/"))
            if "domain" in role and ("." in imported.split("/")[0] and imported != MODULE + "/domain" or concrete):
                raise ValueError("forbidden Domain import: " + name + " -> " + imported)
            if "application" in role and concrete:
                raise ValueError("forbidden Application import: " + name + " -> " + imported)
            if name.startswith(tuple(MODULE + "/" + path for path in ("kernel/", "persistence/uow", "security/scope", "tenancy", "events/"))) and imported.startswith(MODULE + "/modules/"):
                raise ValueError("Core depends on organization implementation/contracts: " + name)


def duration(value):
    match = re.fullmatch(r"([0-9]+(?:\.[0-9]+)?)(ms|s|m|h)", value)
    if not match or float(match[1]) <= 0:
        raise ValueError("invalid positive duration: " + value)
    return float(match[1]) * {"ms": .001, "s": 1, "m": 60, "h": 3600}[match[2]]


def check_config(profile):
    for section in profile.values():
        if not isinstance(section, dict):
            continue
        for key, value in section.items():
            if isinstance(value, (int, float)) and (not math.isfinite(value) or value <= 0):
                raise ValueError("nonpositive profile value: " + key)
            if isinstance(value, str) and (re.fullmatch(r"-?[0-9.]+(?:ms|s|m|h)", value) or key.endswith(("timeout", "retention", "window", "budget")) or key in ("lease", "retry_initial", "retry_max", "alert_oldest_pending")):
                duration(value)
    for key in ("default_timeout", "cleanup_timeout", "hook_timeout"):
        duration(profile["uow"][key])
    if duration(profile["events"]["inbox_retention"]) < max(duration(profile["events"][key]) for key in ("broker_retention", "manual_replay_window")):
        raise ValueError("Inbox retention does not cover configured replay windows")
    if duration(profile["idempotency"]["result_retention"]) < duration(profile["idempotency"]["retry_window"]):
        raise ValueError("idempotency retention is shorter than retry guarantee")


def check_data(root=ROOT):
    import contract_matrix
    matrix = load(root / "tests/contract/subtests.json")
    cases = load(root / "tests/contract/cases.json")["cases"]
    spec = (root / "design/panGOlin-framework_實作規範書.md").read_text(encoding="utf-8-sig")
    rows = {line.split("|")[1].strip(): [part.strip() for part in line.strip("|").split("|")] for line in spec.splitlines() if re.match(r"^\| A\d{2} \|", line)}
    if len(matrix["groups"]) != 95:
        raise ValueError("expected 95 subtest groups")
    for group, case in zip(matrix["groups"], cases):
        if any(group[key] != case[key] for key in ("id", "requirement", "milestone", "status")):
            raise ValueError("case/subtest group drift")
        row = rows[group["id"]]
        if group["fixture"] != "tests/fixtures/manifest.json":
            raise ValueError("unknown group fixture reference")
        if group["source_sha256"] != hashlib.sha256("|".join(row).encode()).hexdigest() or [group["setup"], group["stimulus"], group["expected"]] != row[2:]:
            raise ValueError("specification row changed: " + group["id"])
        names = contract_matrix.SCENARIOS[int(group["id"][1:])].split()
        if [s["id"] for s in group["subtests"]] != [group["id"] + "/" + name for name in names]:
            raise ValueError("subtest decomposition drift")
        if any(s["status"] != "planned" or s["assertion_ref"] != group["id"] + ".expected" for s in group["subtests"]):
            raise ValueError("planned subtest cannot claim runtime success")
    profiles = [load(root / f"tests/contract/profiles/{p}.json") for p in ("tenant-only", "organization-enabled", "replacement-contract")]
    if [p["profile"] for p in profiles] != ["tenant-only", "organization-enabled", "replacement-contract"]:
        raise ValueError("profile identity does not match its manifest path")
    check_profiles(matrix["groups"], profiles)
    fixtures = load(root / "tests/fixtures/manifest.json")
    for name in fixtures["files"]:
        load(root / name)
    scopes = load(root / "tests/fixtures/scopes.json")
    ids = [(item["tenant"],item["kind"],item["id"]) for item in scopes["catalog"]]
    if len(set(ids)) != len(ids) or any((item["tenant"],item["kind"],item["scope"]) not in ids for item in scopes["memberships"] + scopes["enterprise_roles"] + scopes["service_grants"]):
        raise ValueError("fixture catalog references are inconsistent")
    for key, value in scopes["assemblies"].items():
        if set(value["kinds"]) != ({"tenant"} if key == "tenant-only" else {"tenant", "org.enterprise", "org.organization"}):
            raise ValueError("fixture kind capabilities drift")
    modules = load(root / "tests/fixtures/modules.json")
    if modules["layers"] != [["a"], ["b", "c"]] or modules["partial_start_failure"]["ready"]:
        raise ValueError("lifecycle fixture has contradictory observations")
    check_config(load(root / "deploy/profiles/local.json"))
    return {"groups": 95, "named_subtests": sum(len(g["subtests"]) for g in matrix["groups"]), "profiles": 3, "fixture_files": len(fixtures["files"])}


def check(root=ROOT):
    import product_version
    framework_version = product_version.check(root)
    result = check_data(root)
    check_api(load(root / "tests/contract/api.json"), api_surface(root))
    packages = json_stream(command(["go", "list", "-json", "./..."], root))
    check_imports(packages)
    # A separate tenant-only dependency graph must not capture organization ports/implementation.
    tenant = command(["go", "list", "-deps", "./internal/testkit/assemblies/tenant"], root).splitlines()
    if any(name.startswith(MODULE + "/modules/organization") for name in tenant):
        raise ValueError("tenant-only fixture captures organization dependency")
    import dependencies
    dependency_result = dependencies.check()
    return dict(result, api_packages=len(load(root / "tests/contract/api.json")["packages"]), dependencies=dependency_result, framework_version=framework_version)


def revision(root=ROOT):
    return command(["git", "rev-parse", "HEAD"], root).strip()


def clean(root=ROOT):
    return not command(["git", "status", "--porcelain"], root).strip()


def verify(args):
    root = ROOT
    if not clean(root):
        raise ValueError("M0 evidence requires a clean, committed source tree")
    commit = revision(root)
    metadata = json.loads(command(["go", "env", "-json", "GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED", "GOMOD", "GOWORK"], root))
    if metadata["GOVERSION"] != "go" + load(root / "deploy/toolchain.json")["go"]:
        raise ValueError("unexpected Go toolchain")
    platform = metadata["GOOS"] + "-" + metadata["GOARCH"]
    stage = "environment" if args.environment else platform
    output = root / "artifacts" / commit / "m0" / stage
    output.mkdir(parents=True, exist_ok=True)
    report = {"commit": commit, "baseline": "v1.3", "platform": platform, "working_tree_dirty": False, "metadata": metadata, "status": "failed", "commands": [], "runtime_acceptance": "planned"}
    report["inputs"] = {name:digest(root / name) for name in ("deploy/toolchain.json", "deploy/profiles/local.json", "tests/fixtures/manifest.json", "tests/contract/api.json", "tests/contract/subtests.json")}
    try:
        if args.environment:
            commands = [[sys.executable, "scripts/smoke.py", "--project", args.project, "--nats-port", str(args.nats_port), "--nats-proxy-port", str(args.nats_proxy_port), "--idp-port", str(args.idp_port), "--toxi-port", str(args.toxi_port)]]
        else:
            report["inventory"] = check(root)
            commands = [["go", "build", "./..."], ["go", "vet", "./..."], ["go", "run", "./tools/check"], [sys.executable, "-m", "unittest", "discover", "-s", "tests/m0", "-p", "test_*.py"]]
            packages = ",".join(p for p in command(["go", "list", "./..."], root).splitlines() if "/internal/testkit/" not in p)
            profile = output / "unit.out"
            commands += [["go", "test", "-json", "-coverpkg=" + packages, "-covermode=atomic", "-coverprofile=" + str(profile), "./..."], ["go", "run", "./tools/check", "-coverage", str(profile)]]
            commands.append(["go", "run", "./tools/check", "-release"])
            if args.race:
                commands.append(["go", "test", "-race", "./..."])
            report["race"] = "required-pending" if args.race else "not-run-on-this-platform"
        for index, argv in enumerate(commands):
            started = time.monotonic()
            result = subprocess.run(argv, cwd=root, env=go_environment(root), capture_output=True, text=True, encoding="utf-8", errors="replace")
            expected_exit = 1 if argv == ["go", "run", "./tools/check", "-release"] else 0
            log = output / f"{index:02d}.log"
            log.write_text(result.stdout + result.stderr, encoding="utf-8")
            report["commands"].append({"argv": argv, "exit_code": result.returncode, "expected_exit_code": expected_exit, "seconds": round(time.monotonic()-started, 3), "log": log.name, "sha256": digest(log)})
            print("PASS" if result.returncode == expected_exit else "FAIL", " ".join(argv[:3]), flush=True)
            if result.returncode != expected_exit:
                raise ValueError(f"command failed; see {log}")
            if argv[0:3] == ["go", "test", "-json"]:
                events = [json.loads(line) for line in result.stdout.splitlines() if line.startswith("{")]
                if any(event.get("Action") == "skip" and event.get("Test") for event in events):
                    raise ValueError("M0 engineering tests must not be skipped")
        if args.environment:
            smoke = load(root / "artifacts/m0-smoke.json")
            if smoke["commit"] != commit or smoke["working_tree_dirty"] or smoke["status"] != "passed":
                raise ValueError("environment evidence does not match clean source commit")
            shutil.copyfile(root / "artifacts/m0-smoke.json", output / "smoke.json")
            report["environment_checks"] = smoke["checks"]
            result = subprocess.run(["docker", "compose", "-p", args.project, "-f", "deploy/compose/compose.yaml", "logs", "--no-color"], cwd=root, capture_output=True, text=True, encoding="utf-8", errors="replace", check=True)
            (output / "compose.log").write_text(result.stdout + result.stderr, encoding="utf-8")
        elif args.race:
            report["race"] = "passed"
        if not clean(root) or revision(root) != commit:
            raise ValueError("source changed during verification")
        report["status"] = "passed"
    except Exception as error:
        report["error"] = str(error)
    report["artifacts"] = {path.name: digest(path) for path in sorted(output.iterdir()) if path.is_file() and path.name != "report.json"}
    (output / "report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2))
    return 0 if report["status"] == "passed" else 1


def validate_report(report, name, commit, directory):
    if report["commit"] != commit or report["baseline"] != "v1.3" or report["working_tree_dirty"] or report["status"] != "passed" or report["runtime_acceptance"] != "planned" or not report["commands"] or any(c["exit_code"] != (1 if c["argv"] == ["go","run","./tools/check","-release"] else 0) for c in report["commands"]):
        raise ValueError("incomplete/different-commit evidence: " + name)
    if not report["artifacts"] or not report["inputs"]:
        raise ValueError("evidence or input hashes missing")
    for path, expected in report["artifacts"].items():
        if Path(path).name != path or digest(directory / path) != expected:
            raise ValueError("evidence artifact changed: " + path)
    for item in report["commands"]:
        if item["log"] not in report["artifacts"] or item["sha256"] != report["artifacts"][item["log"]]:
            raise ValueError("command log hash mismatch")
    commands = [c["argv"] for c in report["commands"]]
    if name != "environment":
        for prefix in (["go","build"],["go","vet"],["go","run","./tools/check"],["go","test","-json"]):
            if not any(argv[:len(prefix)] == prefix for argv in commands):
                raise ValueError("mandatory engineering command missing")
        if not any("unittest" in argv for argv in commands) or not any("-coverage" in argv for argv in commands):
            raise ValueError("M0 regression/coverage command missing")
        if ["go","run","./tools/check","-release"] not in commands:
            raise ValueError("unimplemented-runtime release guard proof missing")
        if report["platform"] != name:
            raise ValueError("platform evidence mislabeled")
    if name == "linux-amd64" and (report["race"] != "passed" or ["go","test","-race","./..."] not in commands):
        raise ValueError("race command proof missing")
    if name == "environment" and not any("scripts/smoke.py" in argv for argv in commands):
        raise ValueError("environment command proof missing")


def gate(commit):
    root = ROOT / "artifacts" / commit / "m0"
    reports = {}
    for name in ("windows-amd64", "linux-amd64", "environment"):
        report = load(root / name / "report.json")
        validate_report(report, name, commit, root / name)
        reports[name] = report
    if reports["linux-amd64"]["race"] != "passed" or len(reports["environment"].get("environment_checks", [])) < 9:
        raise ValueError("race or environment proof missing")
    if any(report["inputs"] != reports["windows-amd64"]["inputs"] for report in reports.values()):
        raise ValueError("evidence inputs differ across runners")
    result = {"milestone": "M0", "status": "verified", "source_commit": commit, "runtime_requirements": "all-planned", "reports": {name: digest(root / name / "report.json") for name in reports}}
    (root / "gate.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["check", "freeze-api", "verify", "gate"])
    parser.add_argument("--race", action="store_true")
    parser.add_argument("--environment", action="store_true")
    parser.add_argument("--project", default="pangolin-m0")
    parser.add_argument("--commit")
    for name, default in (("nats-port",14222),("nats-proxy-port",14223),("idp-port",18080),("toxi-port",18474)):
        parser.add_argument("--" + name, type=int, default=default)
    args = parser.parse_args()
    try:
        if args.action == "freeze-api":
            (ROOT / "tests/contract/api.json").write_text(json.dumps(api_surface(), ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        elif args.action == "check":
            print(json.dumps(check(), indent=2))
        elif args.action == "verify":
            return verify(args)
        else:
            if not args.commit or not re.fullmatch(r"[a-f0-9]{40}", args.commit):
                raise ValueError("gate requires the full source commit")
            gate(args.commit)
        return 0
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError) as error:
        print("M0 check failed:", error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
