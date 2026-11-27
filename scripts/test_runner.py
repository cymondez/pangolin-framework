"""Core Go/Python test validation, atomic coverage merging, and cumulative runner."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import traceback

import contracts
import e2e

ROOT = contracts.ROOT


def source_hash():
    paths = contracts.command(["git", "-c", "core.quotepath=false", "ls-files", "--cached", "--others", "--exclude-standard"]).splitlines()
    digest = hashlib.sha256()
    for name in sorted(set(paths)):
        path = ROOT / name
        if path.is_file():
            digest.update(name.encode() + b"\0" + path.read_bytes())
    return digest.hexdigest()


def validate_tests(text, required=()):
    events = [json.loads(line) for line in text.splitlines() if line.startswith("{")]
    if not events or any(e.get("Action") == "fail" or e.get("Action") == "skip" and e.get("Test") for e in events):
        raise ValueError("test failure, skip or missing JSON events")
    passed = {e.get("Package", "") + ":" + e.get("Test", "") for e in events if e.get("Action") == "pass"}
    missing = set(required) - passed
    if missing:
        raise ValueError("missing required cases: " + ", ".join(sorted(missing)))
    return sorted(passed)


def validate_python_output(text, required=()):
    count = re.search(r"Ran ([0-9]+) tests?", text)
    if not count or int(count[1]) == 0 or "skipped=" in text or not re.search(r"\nOK\s*$", text):
        raise ValueError("Python regression failed, skipped or did not run")
    if any(name not in text for name in required):
        raise ValueError("required Python acceptance case missing")


def database_packages(milestone, path):
    """Older milestone runners never require the backend suite; M5+ acceptance fails closed without it."""
    if int(milestone[1:]) < 5:
        return []
    if not path.is_dir():
        raise ValueError("database acceptance requires the tests/database backend suite")
    return ["./tests/database"]


def regression_names(field):
    return list(field.values()) if isinstance(field, dict) else list(field)


def required_integration_tests(increments):
    """Baseline flow plus every incremental integration and corrective database case."""
    required = [contracts.MODULE + "/tests/m1:TestM1Flow"]
    for extra in increments:
        required.extend(extra["integration_tests"].values())
        required.extend(regression_names(extra.get("regression_integration_tests", {})))
    return required


def validate_mapping(mapping):
    expected = {s["id"] for g in contracts.load(ROOT / "tests/contract/subtests.json")["groups"] if g["milestone"] == "M1" for s in g["subtests"]}
    go_tests, python_tests = mapping["go_tests"], mapping["python_tests"]
    if set(go_tests) | set(python_tests) != expected or set(go_tests) & set(python_tests) or not all(go_tests.values()) or not all(python_tests.values()):
        raise ValueError("M1 acceptance mapping is incomplete or duplicated")


def check_module_docs(mapping):
    for name in mapping["module_docs"]:
        path = ROOT / name
        if not path.is_file() or not path.read_text(encoding="utf-8-sig").strip():
            raise ValueError("module documentation missing: " + name)
        prose = re.sub(r"```.*?```", "", path.read_text(encoding="utf-8-sig"), flags=re.DOTALL)
        prose = re.sub(r"`[^`\n]+`", "", prose)
        for link in re.findall(r"\]\(([^)]+)\)", prose):
            target = link.split("#")[0]
            if target and not target.startswith(("http:", "https:", "mailto:")) and not (path.parent / target).exists():
                raise ValueError("broken module documentation link: " + name + " -> " + target)


def merge_profiles(paths, output):
    blocks = {}
    for path in paths:
        lines = path.read_text().splitlines()
        if not lines or lines[0] != "mode: atomic":
            raise ValueError("missing atomic suite profile")
        for line in lines[1:]:
            location, statements, count = line.rsplit(" ", 2)
            statements, count = int(statements), int(count)
            if location in blocks and blocks[location][0] != statements:
                raise ValueError("coverage block metadata drift")
            previous = blocks.get(location, (statements, 0))
            blocks[location] = (statements, max(count, previous[1]))
    output.write_text("mode: atomic\n" + "\n".join(f"{k} {s} {c}" for k, (s, c) in sorted(blocks.items())) + "\n")


def validate_commands(commands, race):
    required = {"inventory", "build", "vet", "go-module-verify", "unit", "m0-regression", "m1-regression", "integration", "e2e", "coverage", "generation"}
    if race:
        required.add("race")
    found = {c["name"] for c in commands if c["exit_code"] == 0}
    if not required <= found or any(c["exit_code"] != 0 for c in commands):
        raise ValueError("required engineering gate missing or failed")


def validate_report(report, commit, directory):
    directory = Path(directory)
    if report["commit"] != commit or report["working_tree_dirty"] or report["baseline"] != "v1.3" or report["status"] != "passed":
        raise ValueError("verification requires passed clean same-commit evidence")
    linux = report["platform"]["GOOS"] == "linux"
    validate_commands(report["commands"], linux)
    if linux and report["platform"]["CGO_ENABLED"] != "1":
        raise ValueError("race evidence requires CGO")
    if report["e2e"].get("outbox_attempts") != 2 or report["e2e"].get("inbox_processed") != 1 or not all(
        report["e2e"].get(k) for k in ("producer_consumer_processes", "signed_jwt", "commit_before_2xx", "cross_tenant_rejected", "crash_after_broker_ack")
    ):
        raise ValueError("missing multi-process E2E evidence")
    if not {"unit.out", "integration.out", "e2e.out", "merged.out", "coverage.json", "coverage.html", "go-list.json", "exclusions.json", "dependencies.json"} <= report["artifacts"].keys():
        raise ValueError("required raw evidence missing")
    for name, digest in report["artifacts"].items():
        path = (directory / name).resolve()
        if not path.is_relative_to(directory.resolve()) or not path.is_file() or contracts.digest(path) != digest:
            raise ValueError("evidence hash mismatch: " + name)
    for command in report["commands"]:
        if command["name"] == "e2e":
            continue
        if command.get("log") not in report["artifacts"] or command.get("sha256") != report["artifacts"][command["log"]]:
            raise ValueError("command evidence missing")
    required = contracts.load(ROOT / "tests/contract/m1.json")["go_tests"].values()
    if not set(required) <= set(report["tests"]):
        raise ValueError("required core case evidence missing")


def verify(args, increment=None, extra_steps=None, coverage_steps=None, prepare=None):
    milestone = increment["milestone"] if increment else "M1"
    increments = ([contracts.load(ROOT / ("tests/contract/m" + str(n) + ".json")) for n in range(2, int(milestone[1:]))] + [increment]) if increment else []
    commit = contracts.revision()
    source = source_hash()
    dirty = not contracts.clean()
    platform = json.loads(contracts.command(["go", "env", "-json", "GOOS", "GOARCH", "GOVERSION", "CGO_ENABLED"]))
    if platform["GOVERSION"] != "go" + contracts.load(ROOT / "deploy/toolchain.json")["go"]:
        raise ValueError("unexpected toolchain")
    label = platform["GOOS"] + "-" + platform["GOARCH"]
    # Dirty development runs are inspectable but cannot become Verified evidence.
    output = ROOT / "artifacts" / (milestone.lower() + "-development-" + source[:12] + "/" + label if dirty else commit + "/" + milestone.lower() + "/" + label)
    output.mkdir(parents=True, exist_ok=True)
    if int(milestone[1:]) >= 4:
        os.environ["PANGOLIN_M4_METRICS_DIR"] = str(output)
    report = {
        "milestone": milestone,
        "commit": commit,
        "source_sha256": source,
        "working_tree_dirty": dirty,
        "baseline": "v1.3",
        "platform": platform,
        "requested_race": bool(args.race),
        "seed": 20261121,
        "project": args.project,
        "status": "failed",
        "commands": [],
    }
    mapping = contracts.load(ROOT / "tests/contract/m1.json")
    validate_mapping(mapping)
    check_module_docs(mapping)
    for extra in increments:
        check_module_docs(extra)
    report["inputs"] = {
        name: contracts.digest(ROOT / name)
        for name in ("VERSION", "go.mod", "go.sum", "deploy/toolchain.json", "deploy/profiles/local.json", "deploy/dependencies.json", "tests/fixtures/manifest.json", "tests/contract/api.json", "tests/contract/m1.json")
    }
    for extra in increments:
        name = "tests/contract/" + extra["milestone"].lower() + ".json"
        report["inputs"][name] = contracts.digest(ROOT / name)

    def execute(argv, name=None, env=None):
        index = len(report["commands"])
        name = name or "e2e-step"
        log = output / f"{index:02d}-{name}.log"
        started = time.monotonic()
        result = subprocess.run(argv, cwd=ROOT, env=env or contracts.go_environment(ROOT), capture_output=True, text=True, encoding="utf-8", errors="replace")
        log.write_text(result.stdout + result.stderr, encoding="utf-8")
        report["commands"].append({"name": name, "argv": argv, "exit_code": result.returncode, "log": log.name, "sha256": contracts.digest(log), "seconds": round(time.monotonic() - started, 3)})
        if result.returncode:
            raise ValueError("gate failed: " + name + "; see " + str(log))
        if name in {"m0-regression", "m1-regression", "m2-regression", "m3-regression", "m4-regression", "m5-regression", "m6-regression", "m7-regression"}:
            required = mapping["python_tests"].values() if name == "m1-regression" else next((x["python_tests"].values() for x in increments if name == x["milestone"].lower() + "-regression"), ())
            validate_python_output(result.stdout + result.stderr, required)
        print("PASS", name, flush=True)
        return result.stdout

    try:
        execute([sys.executable, "scripts/contracts.py", "check"], "inventory")
        go_dirs = contracts.go_packages()
        build_dirs = contracts.go_build_packages()
        (output / "go-list.json").write_text(contracts.command(["go", "list", "-json", *go_dirs]), encoding="utf-8")
        (output / "exclusions.json").write_bytes((ROOT / "tests/coverage/exclusions.json").read_bytes())
        (output / "dependencies.json").write_bytes((ROOT / "deploy/dependencies.json").read_bytes())
        execute(["go", "build", *build_dirs], "build")
        execute(["go", "vet", *go_dirs], "vet")
        execute(["go", "mod", "verify"], "go-module-verify")
        execute([sys.executable, "-m", "unittest", "discover", "-v", "-s", "tests/m0", "-p", "test_*.py"], "m0-regression")
        execute([sys.executable, "-m", "unittest", "discover", "-v", "-s", "tests/m1", "-p", "test_*.py"], "m1-regression")
        packages = ",".join(p for p in contracts.command(["go", "list", *go_dirs]).splitlines() if "/internal/testkit/" not in p and "/infrastructure/internal/" not in p)
        if prepare:
            report["prepare"] = prepare(report, output, execute, packages) or report.get("prepare")
        for extra in increments:
            name = extra["milestone"].lower()
            execute([sys.executable, "-m", "unittest", "discover", "-v", "-s", "tests/" + name, "-p", "test_*.py"], name + "-regression")
        unit = execute(["go", "test", "-count=1", "-json", "-covermode=atomic", "-coverpkg=" + packages, "-coverprofile=" + str(output / "unit.out"), *go_dirs], "unit")
        report["tests"] = validate_tests(unit, [*mapping["go_tests"].values(), *(test for extra in increments for test in [*extra["unit_tests"].values(), *extra.get("regression_tests", [])])])
        integration = execute(["go", "test", "-p=1", "-count=1", "-json", "-tags=integration", "-covermode=atomic", "-coverpkg=" + packages, "-coverprofile=" + str(output / "integration.out"), "./tests/m1", *("./tests/" + extra["milestone"].lower() for extra in increments), *database_packages(milestone, ROOT / "tests/database")], "integration")
        report["integration_tests"] = validate_tests(integration, required_integration_tests(increments))
        report["e2e"] = e2e.run(output, args.project, packages, execute)
        report["commands"].append({"name": "e2e", "exit_code": 0})
        profiles = [output / "unit.out", output / "integration.out", output / "e2e.out"]
        if milestone in ("M5", "M6", "M7"):
            execute([sys.executable, "scripts/template_bundle.py", "--check"], "template-source")
            cli = output / ("pangolin.exe" if os.name == "nt" else "pangolin")
            execute(["go", "build", "-cover", "-covermode=atomic", "-coverpkg=" + packages, "-o", str(cli), "./cmd/pangolin"], "cli-build")
            covered = output / "cli-cov"
            covered.mkdir(exist_ok=True)
            previous = os.environ.get("GOCOVERDIR")
            os.environ["GOCOVERDIR"] = str(covered)
            try:
                execute([sys.executable, "scripts/templates_verify.py", "templates", "--output", str(output), "--cli", str(cli)], "templates")
            finally:
                if previous is None:
                    os.environ.pop("GOCOVERDIR", None)
                else:
                    os.environ["GOCOVERDIR"] = previous
            execute(["go", "tool", "covdata", "textfmt", "-i=" + str(covered), "-o=" + str(output / "cli.out")], "cli-coverage")
            profiles.append(output / "cli.out")
            if os.name == "nt" or os.getenv("PANGOLIN_M5_COMPOSE") == "1":
                execute([sys.executable, "scripts/templates_verify.py", "compose", "--output", str(output), "--project", args.project], "templates-compose")
        if args.race:
            race = execute(["go", "test", "-p=1", "-count=1", "-json", "-race", "-tags=integration", *contracts.go_packages(tags=True)], "race")
            validate_tests(race)
        sqlc = os.getenv("PANGOLIN_SQLC", str(ROOT / ".cache/bin" / ("sqlc.exe" if os.name == "nt" else "sqlc")))
        if execute([sqlc, "version"], "generation-version").strip() != "v1.31.1":
            raise ValueError("wrong sqlc version")
        generated = list((ROOT / "examples/bookstore/infrastructure/internal").glob("*/*.go"))
        before = {str(p.relative_to(ROOT)): contracts.digest(p) for p in generated}
        execute([sqlc, "generate"], "generation")
        if before != {str(p.relative_to(ROOT)): contracts.digest(p) for p in generated}:
            raise ValueError("sqlc generated source drift")
        if coverage_steps:
            profiles.extend(coverage_steps(report, output, execute, packages))
        merge_profiles(profiles, output / "merged.out")
        counts = execute(["go", "run", "./tools/check", "-coverage", str(output / "merged.out")], "coverage")
        (output / "coverage.json").write_text(json.dumps(json.loads(counts.splitlines()[0]), indent=2) + "\n", encoding="utf-8")
        execute(["go", "tool", "cover", "-html=" + str(output / "merged.out"), "-o=" + str(output / "coverage.html")], "coverage-html")
        validate_commands(report["commands"], args.race)
        if extra_steps:
            extra_steps(report, output, execute)
        if source_hash() != source or contracts.revision() != commit:
            raise ValueError("source changed during verification")
        report["status"] = "passed"
        report["gate"] = "development-only" if dirty or platform["GOOS"] == "linux" and not args.race else "evidence-ready"
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        report["error"] = str(error)
        (output / "failure.log").write_text(traceback.format_exc(), encoding="utf-8")
    report["artifacts"] = {str(p.relative_to(output)): contracts.digest(p) for p in output.rglob("*") if p.is_file() and p.name != "report.json"}
    (output / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({"status": report["status"], "error": report.get("error"), "report": str(output / "report.json")}, indent=2), flush=True)
    return 0 if report["status"] == "passed" else 1
