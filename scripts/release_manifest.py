"""Release manifest validation and same-commit release evidence collection.

The committed manifest (deploy/release/manifest.json) declares the v0.1.0
release: baseline, product version, 95 unique requirement deliveries, 532
named subtests (431 legacy plus 10 A63/permit-* and 101 A01/A02/A46/A74 root-*
supplemental), three profile applicability rows, runtime/CLI/template version
bindings, published event schemas, critical coverage packages, required
release assets and release drills. Every declaration is re-validated against
authoritative sources (specification, milestone plan, contract files, Go
sources); nothing passes because it was merely written down. Run-time release
evidence is machine-collected with hashes only and fails closed on missing,
skipped or sub-threshold inputs.
"""
import argparse
import json
import math
from pathlib import Path
import re
import sys

import m0

ROOT = m0.ROOT
BASELINE = "v1.3"
PRODUCT_VERSION = "0.1.0"
VERSION_SCOPE = "v0.1.0"
# 431 legacy named subtests plus the 2026-11-25 supplemental root-DI families
# (A63/permit-*, A01/A02/A46 root-* incl. Stop-resolver visibility, explicit
# module.OpenScope suites, non-owner binding and foundation typed injection);
# the total is derived in contract_matrix.
NAMED_SUBTESTS = 532
MANIFEST_REL = "deploy/release/manifest.json"
GATES = ("windows-amd64", "linux-amd64")
PROFILES = ("tenant-only", "organization-enabled", "replacement-contract")
DRILLS = ("clean-rebuild", "forward-upgrade", "backup-restore", "load-baseline", "security-review")
# Spec 14.1 threat model categories; each needs attack surface, abuse path,
# controls, executable tests and residual risk in the security review drill.
THREAT_CATEGORIES = (
    "identity-impersonation", "tenant-header-forgery", "sql-http-injection",
    "cross-scope-idor", "governance-role-impersonation", "disabled-module-owner-downgrade",
    "duplicate-event-double-charge", "worker-retry-storm", "unauthorized-host-exception",
    "secret-leak", "cache-poisoning", "excessive-database-privilege",
)
# Release deliverables that must be listed and present (M6 WP18, spec section 8).
REQUIRED_ASSETS = (
    "deploy/dependencies.json",
    "deploy/toolchain.json",
    "deploy/profiles/local.json",
    "deploy/compose/compose.yaml",
    "deploy/compose/m1.yaml",
    "deploy/compose/m5-databases.yaml",
    "tests/coverage/exclusions.json",
    "docs/runbooks/operations.md",
    "docs/runbooks/release.md",
    "docs/security/release-review.md",
    "docs/release-notes/0.1.0.md",
    "deploy/docker/Dockerfile.m6-runner",
    "deploy/docker/README.md",
    "docs/guides/m6-development.md",
)
EVENT_TYPE_PATTERN = r"[a-z][a-z0-9_]*\.[a-z][a-z0-9_.]*"


def _production_sources(root, directories):
    root = Path(root)
    for directory in directories:
        for path in (root / directory).rglob("*.go"):
            if path.name.endswith("_test.go") or "/cmd/" in path.relative_to(root).as_posix() or "/testkit/" in path.relative_to(root).as_posix():
                continue
            yield path


def check_structure(manifest):
    keys = ("baseline", "product_version", "version_scope", "requirements", "subtests",
            "profiles", "deliveries", "version_bindings", "event_schemas",
            "critical_packages", "assets", "drills", "gates")
    missing = [key for key in keys if key not in manifest]
    if missing or set(manifest) - set(keys):
        raise ValueError("release manifest keys missing or unknown: " + ", ".join(missing or sorted(set(manifest) - set(keys))))
    if manifest["baseline"] != BASELINE or manifest["product_version"] != PRODUCT_VERSION or manifest["version_scope"] != VERSION_SCOPE:
        raise ValueError("release baseline/version scope drift")
    if manifest["requirements"] != 95 or manifest["subtests"] != NAMED_SUBTESTS:
        raise ValueError("release must bind 95 requirements and " + str(NAMED_SUBTESTS) + " named subtests")
    if [p["profile"] for p in manifest["profiles"]] != list(PROFILES):
        raise ValueError("release profiles must list all three applicability rows")
    for profile in manifest["profiles"]:
        if set(profile) != {"profile", "applicability_source"}:
            raise ValueError("invalid profile row: " + profile.get("profile", "?"))
        if profile["applicability_source"] != f"tests/contract/profiles/{profile['profile']}.json":
            raise ValueError("profile applicability source drift: " + profile["profile"])
    if len(manifest["deliveries"]) != 95:
        raise ValueError("release deliveries must cover 95 requirements")
    for requirement, delivery in manifest["deliveries"].items():
        if set(delivery) != {"milestone", "case"} or not re.fullmatch(r"M[0-6]", delivery["milestone"]) or not re.fullmatch(r"A[0-9]{2}", delivery["case"]):
            raise ValueError("invalid delivery entry: " + requirement)
    bindings = manifest["version_bindings"]
    if set(bindings) != {"runtime", "cli", "template"}:
        raise ValueError("version bindings must cover runtime, CLI and template")
    expected_binding = {"runtime": ("file", "VERSION"), "cli": ("file", "internal/scaffold/cli.go"), "template": ("source", "internal/scaffold/assets")}
    for name, (key, path) in expected_binding.items():
        binding = bindings[name]
        if not {"version", key} <= set(binding) or binding[key] != path or binding["version"] != PRODUCT_VERSION:
            raise ValueError("version binding drift: " + name)
    if set(bindings["template"]) != {"version", "source", "runtime_require"} or bindings["template"]["runtime_require"] != "v" + PRODUCT_VERSION:
        raise ValueError("template must bind the runtime require version")
    schemas = manifest["event_schemas"]
    if not schemas.get("policy") or not isinstance(schemas.get("entries"), list) or not schemas["entries"]:
        raise ValueError("published event schemas must declare a policy and entries")
    seen = set()
    for entry in schemas["entries"]:
        if set(entry) != {"type", "schema_version", "producer", "source"}:
            raise ValueError("invalid event schema entry: " + str(entry.get("type")))
        if not re.fullmatch(EVENT_TYPE_PATTERN, entry["type"]) or entry["type"] in seen:
            raise ValueError("invalid or duplicate event schema type: " + str(entry.get("type")))
        seen.add(entry["type"])
        if not isinstance(entry["schema_version"], int) or isinstance(entry["schema_version"], bool) or entry["schema_version"] < 1:
            raise ValueError("event schema version must be a positive wire version: " + entry["type"])
        if entry["producer"] not in ("bookstore", "organization"):
            raise ValueError("unknown event producer: " + entry["producer"])
    for key in ("critical_packages", "assets"):
        values = manifest[key]
        if not values or len(set(values)) != len(values) or any(not isinstance(value, str) or not value for value in values):
            raise ValueError(f"release {key} must be a non-empty unique list")
    if set(manifest["drills"]) != set(DRILLS) or len(manifest["drills"]) != len(DRILLS):
        raise ValueError("release drills must list all five executable drills")
    if manifest["gates"] != list(GATES):
        raise ValueError("release gates must cover both verified platforms")
    return {"requirements": manifest["requirements"], "subtests": manifest["subtests"], "profiles": 3, "drills": len(manifest["drills"])}


def check_deliveries(manifest, root=ROOT):
    root = Path(root)
    cases = m0.load(root / "tests/contract/cases.json")["cases"]
    groups = m0.load(root / "tests/contract/subtests.json")["groups"]
    plan = (root / "design/panGOlin-framework_里程碑規劃書.md").read_text(encoding="utf-8-sig")
    rows = re.findall(r"^\| (M[0-6]) \| ([A-Z]+-[0-9]{3}) \| (A[0-9]{2})；", plan, re.M)
    if len(rows) != 95:
        raise ValueError("milestone delivery table does not bind 95 requirements")
    plan_delivery = {requirement: (milestone, case) for milestone, requirement, case in rows}
    case_owner = {case["id"]: case["requirement"] for case in cases}
    deliveries = manifest["deliveries"]
    unknown = sorted(set(deliveries) - set(plan_delivery))
    if unknown:
        raise ValueError("unknown requirement in release deliveries: " + ", ".join(unknown))
    missing = sorted(set(plan_delivery) - set(deliveries))
    if missing:
        raise ValueError("missing requirement in release deliveries: " + ", ".join(missing))
    owners = {}
    for requirement, delivery in deliveries.items():
        case = delivery["case"]
        if owners.setdefault(case, requirement) != requirement:
            raise ValueError("duplicate delivery: case " + case + " claimed by " + owners[case] + " and " + requirement)
        if case_owner.get(case) != requirement:
            raise ValueError("delivery drift: " + requirement + " does not own case " + case)
    group_by_requirement = {group["requirement"]: group for group in groups}
    for requirement, delivery in deliveries.items():
        milestone, case = plan_delivery[requirement]
        group = group_by_requirement.get(requirement)
        if (delivery["milestone"], delivery["case"]) != (milestone, case) or not group or group["id"] != case or group["milestone"] != milestone:
            raise ValueError("delivery drift: " + requirement + " disagrees with the milestone plan or contract cases")
    return len(deliveries)


def profile_rows(root=ROOT):
    root = Path(root)
    groups = m0.load(root / "tests/contract/subtests.json")["groups"]
    profiles = [m0.load(root / f"tests/contract/profiles/{name}.json") for name in PROFILES]
    m0.check_profiles(groups, profiles)
    return [{"profile": name, "required": sum(1 for entry in profile["subtests"] if entry["applicability"] == "required"),
             "not_applicable": sum(1 for entry in profile["subtests"] if entry["applicability"] != "required")}
            for name, profile in zip(PROFILES, profiles)]


def check_profiles(manifest, root=ROOT):
    rows = profile_rows(root)
    if {row["profile"] for row in rows} != {p["profile"] for p in manifest["profiles"]}:
        raise ValueError("manifest profile rows disagree with the profile manifests")
    return rows


def check_versions(manifest, root=ROOT):
    import product_version
    root = Path(root)
    version = product_version.check(root)
    if version != manifest["product_version"]:
        raise ValueError("declared product version drift: " + version)
    cli_match = re.search(r'const Version\s*=\s*"([^"]+)"', (root / "internal/scaffold/cli.go").read_text(encoding="utf-8-sig"))
    if not cli_match or cli_match[1] != version:
        raise ValueError("CLI version differs from the runtime VERSION")
    template = root / "internal/scaffold/assets/common/go.mod.tmpl"
    if not template.is_file():
        raise ValueError("template module file missing")
    template_match = re.search(r"require github.com/pangolin-framework/pangolin (v[0-9]+\.[0-9]+\.[0-9]+)", template.read_text(encoding="utf-8-sig"))
    if not template_match or template_match[1] != "v" + version:
        raise ValueError("template runtime require version differs from the runtime VERSION")
    return {"runtime": version, "cli": cli_match[1], "template": version, "template_runtime_require": template_match[1]}


def check_event_schemas(manifest, root=ROOT):
    root = Path(root)
    entries = manifest["event_schemas"]["entries"]
    declared = {entry["type"]: entry for entry in entries}
    for entry in entries:
        source = root / entry["source"]
        if not source.is_file() or ('"' + entry["type"] + '"') not in source.read_text(encoding="utf-8-sig"):
            raise ValueError("published event schema not found in its source: " + entry["type"])
    # The generated organization-enabled profile is the publishing deployment
    # unit for organization events; the Bookstore example publishes its own.
    template = root / "internal/scaffold/assets/organization-enabled/foundation/profile.go.tmpl"
    organization = re.search(r"for _, name := range \[\]string\{([^}]*)\}", template.read_text(encoding="utf-8-sig"))
    organization_version = re.search(r"Version:\s*([0-9]+)", template.read_text(encoding="utf-8-sig"))
    if not organization or not organization_version:
        raise ValueError("organization template schema registry is unreadable")
    expected = {name.strip().strip('"') for name in organization.group(1).split(",")}
    books = (root / "examples/bookstore/application/books.go").read_text(encoding="utf-8-sig")
    bookstore = re.search(r'Type:\s*"(' + EVENT_TYPE_PATTERN + r')",\s*SchemaVersion:\s*([0-9]+)', books)
    if not bookstore:
        raise ValueError("Bookstore published envelope is unreadable")
    expected.add(bookstore[1])
    unknown = sorted(set(declared) - expected)
    missing = sorted(expected - set(declared))
    if unknown or missing:
        raise ValueError("event schema entries unknown (" + ", ".join(unknown) + ") or missing (" + ", ".join(missing) + ")")
    for entry in entries:
        wire = int(organization_version[1]) if entry["producer"] == "organization" else int(bookstore[2])
        if entry["schema_version"] != wire:
            raise ValueError("event schema wire version drift: " + entry["type"])
    # Reverse scan: every published type literal must be declared.
    for path in _production_sources(root, ("modules/organization", "examples/bookstore")):
        for line in path.read_text(encoding="utf-8-sig").splitlines():
            for literal in re.findall(r'"(' + EVENT_TYPE_PATTERN + r')"', line):
                if (".emit(" in line or "Type:" in line) and literal not in declared:
                    raise ValueError("undeclared published event schema: " + literal + " in " + str(path))
    return len(entries)


def check_critical(manifest, root=ROOT):
    root = Path(root)
    main = (root / "tools/check/main.go").read_text(encoding="utf-8-sig")
    tool = re.search(r"for _, key := range \[\]string\{([^}]*)\}", main)
    if not tool:
        raise ValueError("critical package list is unreadable from the coverage gate")
    expected = sorted({key.strip().strip('"') for key in tool.group(1).split(",")})
    if sorted(manifest["critical_packages"]) != expected:
        raise ValueError("critical package list drifts from the coverage gate")
    spec = (root / "design/panGOlin-framework_實作規範書.md").read_text(encoding="utf-8-sig")
    section = spec.split("### 13.3")[1].split("### 13.4")[0]
    prefixes = {match for match in re.findall(r"`([a-z/]+?)/\.\.\.`", section)}
    if not prefixes or not prefixes <= set(manifest["critical_packages"]):
        raise ValueError("critical package list omits specification 13.3 prefixes")
    return list(expected)


def check_assets(manifest, root=ROOT):
    root = Path(root)
    listed = manifest["assets"]
    for name in listed:
        if not (root / name).resolve().is_relative_to(root.resolve()):
            raise ValueError("release asset escapes the source tree: " + name)
    missing = [path for path in REQUIRED_ASSETS if path not in listed]
    if missing:
        raise ValueError("missing release item(s): " + ", ".join(missing))
    absent = [name for name in listed if not (root / name).is_file() or (root / name).stat().st_size == 0]
    if absent:
        raise ValueError("missing release asset(s): " + ", ".join(absent))
    return {name: m0.digest(root / name) for name in listed}


def load_mappings(root=ROOT):
    root = Path(root)
    return {number: m0.load(root / f"tests/contract/m{number}.json") for number in range(1, 7)}


def _milestone_fields(mapping):
    fields = ["unit_tests", "integration_tests", "python_tests"]
    if mapping.get("milestone") == "M1":
        fields = ["go_tests", "python_tests"]
    else:
        fields.append("template_tests")
    if mapping.get("milestone") == "M6":
        fields.append("drills")
    if mapping.get("milestone") == "M7":
        fields.append("gate_checks")
    return fields


def check_closure(mappings, groups):
    """Acceptance closure scoped by the newest milestone of the supplied mappings.

    Historical callers keep their original semantics: passing the M1-M6
    mappings still expects exactly the M1-M6 named subtests even on a tree
    whose subtests.json also carries a later appendix.
    """
    numbers = sorted(number for number in mappings if isinstance(number, int))
    scope = max((int(mappings[number]["milestone"][1:]) for number in numbers
                 if isinstance(mappings[number].get("milestone"), str) and mappings[number]["milestone"][1:].isdigit()), default=0)
    scoped = [group for group in groups
              if isinstance(group.get("milestone"), str) and group["milestone"][1:].isdigit() and int(group["milestone"][1:]) <= scope]
    expected = {subtest["id"] for group in scoped for subtest in group["subtests"]}
    milestone_of = {group["id"]: group["milestone"] for group in scoped}
    collected = {}
    result = {}
    for number in numbers:
        mapping = mappings[number]
        if mapping.get("milestone") != f"M{number}" or mapping.get("baseline") != BASELINE:
            raise ValueError(f"M{number} acceptance mapping is mislabeled")
        ids = set()
        for field in _milestone_fields(mapping):
            for subtest, name in mapping.get(field, {}).items():
                if not isinstance(name, str) or not name:
                    raise ValueError("empty case mapping: " + subtest)
                if subtest not in expected:
                    raise ValueError("unknown subtest in M" + str(number) + " mapping: " + subtest)
                owners = collected.setdefault(subtest, [])
                if number not in owners:
                    owners.append(number)
                ids.add(subtest)
        result[f"M{number}"] = len(ids)
    duplicates = sorted(subtest for subtest, owners in collected.items() if len(owners) > 1)
    if duplicates:
        raise ValueError("duplicate subtest across milestones: " + ", ".join(f"{subtest} (M{','.join(str(owner) for owner in collected[subtest])})" for subtest in duplicates))
    for subtest, owners in collected.items():
        if milestone_of[subtest.split("/")[0]] != f"M{owners[0]}":
            raise ValueError("subtest mapped to the wrong milestone: " + subtest)
    missing = sorted(expected - set(collected))
    if missing:
        raise ValueError("missing subtest in acceptance mappings: " + ", ".join(missing))
    return {"mapped": len(collected), "milestones": result}


def check_report_closure(report, mappings, groups):
    closure = check_closure(mappings, groups)
    unit = set(report["tests"])
    integration = set(report["integration_tests"])
    for number in range(1, 7):
        mapping = mappings[number]
        for field, executed in (("unit_tests", unit), ("integration_tests", integration)):
            for subtest, name in mapping.get(field, {}).items():
                if name not in executed:
                    raise ValueError(f"missing required case for {subtest}: {name}")
        for subtest, name in mapping.get("go_tests", {}).items():
            if name not in unit:
                raise ValueError(f"missing required case for {subtest}: {name}")
    return closure


def validate_regression_logs(report, directory, extra=None):
    """Re-validate every milestone regression log from its command evidence.

    Called inside the m1 hook before the final artifact map exists, so log
    hashes are recomputed directly and bound by the finalized report later.
    `extra` names additional acceptance regressions (e.g. {"m7-regression": set()})
    owned by later additive milestones; historical callers are unchanged.
    """
    directory = Path(directory)
    required = {}
    for number in range(1, 7):
        mapping = m0.load(ROOT / f"tests/contract/m{number}.json")
        required[f"m{number}-regression"] = set(mapping.get("python_tests", {}).values())
    required["m0-regression"] = set()
    required.update({name: set(tests) for name, tests in (extra or {}).items()})
    result = {}
    import m1
    for name, expected_tests in required.items():
        commands = [command for command in report["commands"] if command["name"] == name]
        if len(commands) != 1 or commands[0]["exit_code"] != 0:
            raise ValueError("missing or failed regression evidence: " + name)
        command = commands[0]
        log = directory / command["log"]
        if not log.is_file() or m0.digest(log) != command["sha256"]:
            raise ValueError("regression log hash mismatch: " + name)
        m1.validate_python_output(log.read_text(encoding="utf-8-sig"), expected_tests)
        result[name] = len(expected_tests)
    return result


def _statement_counts(value, label):
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise ValueError("coverage counts must be non-negative integers: " + label)
    return value


def release_coverage(coverage, critical_packages, module=m0.MODULE):
    overall = coverage["overall"]
    total = _statement_counts(overall.get("total_statements"), "overall.total")
    covered = _statement_counts(overall.get("covered_statements"), "overall.covered")
    if covered > total:
        raise ValueError("overall coverage counts are impossible")
    if total <= 0 or covered * 100 < total * 90:
        raise ValueError("overall statement coverage is below 90% on raw counts")
    packages = {}
    for package, counts in coverage["packages"].items():
        if not package.startswith(module + "/"):
            raise ValueError("coverage contains a foreign package: " + package)
        package_total = _statement_counts(counts.get("total_statements"), package + ".total")
        package_covered = _statement_counts(counts.get("covered_statements"), package + ".covered")
        if package_covered > package_total:
            raise ValueError("coverage counts are impossible for " + package)
        packages[package[len(module) + 1:]] = {"covered_statements": package_covered, "total_statements": package_total}
    critical = []
    for prefix in critical_packages:
        matched = sorted(name for name in packages if name == prefix or name.startswith(prefix + "/"))
        if not matched:
            raise ValueError("missing critical package coverage: " + prefix)
        for name in matched:
            counts = packages[name]
            package_total, package_covered = counts["total_statements"], counts["covered_statements"]
            if package_total <= 0 or package_covered * 100 < package_total * 90:
                raise ValueError("critical package " + name + " is below 90% on raw counts")
            critical.append({"package": module + "/" + name, "covered_statements": package_covered, "total_statements": package_total})
    return {"overall": {"covered_statements": covered, "total_statements": total},
            "critical": sorted(critical, key=lambda entry: entry["package"])}


def _drill_checks(name, checks):
    def flag(*keys):
        for key in keys:
            if key not in checks:
                raise ValueError("drill " + name + " is missing required evidence: " + key)
            if checks[key] is not True:
                raise ValueError("drill " + name + " failed its " + key + " assertion")

    def count(key, minimum):
        if key not in checks or not isinstance(checks[key], int) or isinstance(checks[key], bool) or checks[key] < minimum:
            raise ValueError("drill " + name + " is missing measured " + key)

    def text(*keys):
        for key in keys:
            if key not in checks or not isinstance(checks[key], str) or not checks[key].strip():
                raise ValueError("drill " + name + " is missing recorded " + key)

    if name == "clean-rebuild":
        flag("clean_source", "clean_database", "clean_build", "tests_passed")
    elif name == "forward-upgrade":
        count("seeded_tenant_rows", 1)
        flag("owner_preserved", "force_rls")
        if checks.get("permissive_policy_left") is not False:
            raise ValueError("forward-upgrade must prove no permissive RLS policy is left")
    elif name == "backup-restore":
        flag("restore_verified", "row_counts_match", "checksums_match", "independent_connection")
    elif name == "load-baseline":
        text("hardware", "go_version", "database", "broker", "methodology")
        count("pool_size", 1)
        count("load_count", 1)
        count("concurrency", 1)
        for key in ("p95_ms", "p99_ms"):
            value = checks.get(key)
            if not isinstance(value, (int, float)) or isinstance(value, bool) or not math.isfinite(value) or value <= 0:
                raise ValueError("load-baseline is missing measured " + key)
        samples = checks.get("samples")
        if (not isinstance(samples, list) or len(samples) < 10
                or any(not isinstance(sample, (int, float)) or isinstance(sample, bool) or not math.isfinite(sample) or sample <= 0 for sample in samples)):
            raise ValueError("load-baseline must include raw latency samples")
        if len(samples) != checks["load_count"]:
            raise ValueError("load-baseline sample count contradicts its measured operation count")
        ordered = sorted(samples)
        for rank, key in ((95, "p95_ms"), (99, "p99_ms")):
            if checks[key] != ordered[(len(ordered) * rank) // 100]:
                raise ValueError("load-baseline percentile does not match the raw samples: " + key)
        fastest, slowest = min(samples), max(samples)
        if checks["p95_ms"] > checks["p99_ms"] or not fastest <= checks["p95_ms"] <= slowest or not fastest <= checks["p99_ms"] <= slowest:
            raise ValueError("load-baseline percentiles contradict the recorded samples")
    elif name == "security-review":
        categories = checks.get("categories")
        if not isinstance(categories, list) or set(categories) != set(THREAT_CATEGORIES):
            raise ValueError("security-review must cover every specification 14.1 threat category")
        if checks.get("unresolved_high_risk") != 0:
            raise ValueError("unresolved high-risk security findings block the release")
        controls = checks.get("controls")
        if not isinstance(controls, list) or not controls:
            raise ValueError("security-review must record concrete controls")
        covered = set()
        for control in controls:
            if set(control) != {"category", "attack_surface", "abuse_path", "control", "test", "residual_risk"}:
                raise ValueError("invalid security control record")
            if control["category"] not in THREAT_CATEGORIES or any(not isinstance(control[key], str) or not control[key].strip() for key in ("attack_surface", "abuse_path", "control", "test", "residual_risk")):
                raise ValueError("security control is incomplete for " + control["category"])
            covered.add(control["category"])
        if covered != set(THREAT_CATEGORIES):
            raise ValueError("security-review leaves threat categories without controls")


def validate_drills(data, directory, commit, source_sha256, platform, project=None, host_run=False, dirty=False):
    """Validate a release-drills.json against the documented drill interface.

    The drills worker owns the report's rich `evidence` content and re-validates
    it in scripts/m6_drills.py; this gate independently re-checks provenance
    (commit/source bytes/dirty class), the recorded platform, every command
    log hash, every artifact hash and the release-blocking `checks` per drill.
    """
    directory = Path(directory)
    if not isinstance(data, dict) or data.get("status") != "passed":
        raise ValueError("drill report must record a passed status")
    if data.get("baseline") != BASELINE or data.get("product_version") != PRODUCT_VERSION:
        raise ValueError("drill report baseline/product version drift")
    if data.get("commit") != commit or data.get("source_sha256") != source_sha256:
        raise ValueError("drill provenance does not match the verified source commit/bytes")
    if data.get("working_tree_dirty") is not bool(dirty) or data.get("evidence_class") != ("development-only" if dirty else "evidence-ready"):
        raise ValueError("drill evidence class does not match the verifying run (clean commits require evidence-ready drills)")
    run_platform = data.get("platform")
    if isinstance(run_platform, dict):
        run_platform = run_platform.get("GOOS") + "-" + run_platform.get("GOARCH") if run_platform.get("GOOS") in ("windows", "linux") and run_platform.get("GOARCH") == "amd64" else None
    if run_platform not in GATES:
        raise ValueError("drill report platform is unknown: " + str(data.get("platform"))[:120])
    details = data.get("platform_details")
    if isinstance(details, dict) and "GOOS" in details and "GOARCH" in details:
        if details["GOOS"] + "-" + details["GOARCH"] != run_platform:
            raise ValueError("drill platform details contradict the recorded platform label")
    if not host_run and run_platform != platform:
        raise ValueError("inline drills must run on the verifying platform")
    if project is not None and data.get("project") != project:
        raise ValueError("drill project mismatch: " + str(data.get("project")))
    if not isinstance(data.get("project"), str) or not re.fullmatch(r"pangolin-m[67]-[a-z0-9-]+", data["project"]):
        raise ValueError("drills must use a dedicated pangolin-m6-* or pangolin-m7-* project")

    def command_log(command, scope):
        if not isinstance(command, dict) or not {"argv", "exit_code", "log", "sha256"} <= set(command):
            raise ValueError("invalid drill command record: " + scope)
        if not isinstance(command["argv"], list) or not command["argv"] or not all(isinstance(part, str) and part for part in command["argv"]):
            raise ValueError("drill command argv is unsafe or empty: " + scope)
        if command["exit_code"] != 0:
            raise ValueError("release drill command failed: " + scope)
        log = directory / command["log"]
        if not log.resolve().is_relative_to(directory.resolve()) or not log.is_file() or m0.digest(log) != command["sha256"]:
            raise ValueError("drill command log is missing or its hash mismatch: " + scope)

    for command in data.get("commands") or []:
        command_log(command, "report")
    artifacts = data.get("artifacts") or {}
    if not isinstance(artifacts, dict) or not artifacts:
        raise ValueError("drill report must hash its evidence artifacts")
    for name, digest in artifacts.items():
        if not isinstance(name, str) or not name.strip() or "\x00" in name:
            raise ValueError("drill artifact name is invalid: " + str(name)[:80])
        path = directory / name
        if not path.resolve().is_relative_to(directory.resolve()) or not path.is_file() or m0.digest(path) != digest:
            raise ValueError("drill artifact missing or hash mismatch: " + name)
    entries = data.get("drills")
    if not isinstance(entries, dict) or set(entries) != set(DRILLS):
        raise ValueError("drill report must cover exactly the five release drills")
    result = []
    for name in DRILLS:
        entry = entries[name]
        if not isinstance(entry, dict) or entry.get("status") != "passed":
            raise ValueError("release drill failed or was skipped (status=" + str(entry.get("status") if isinstance(entry, dict) else "missing") + "): " + name)
        commands = entry.get("commands")
        if not isinstance(commands, list) or not commands:
            raise ValueError("release drill has no command evidence: " + name)
        for command in commands:
            command_log(command, name)
        # The drill worker's script re-validates each entry's own evidence
        # content; this gate independently enforces the release-blocking
        # `checks` and the hashes of every command log and artifact.
        _drill_checks(name, entry.get("checks"))
        result.append({"name": name, "status": "passed", "commands": len(commands), "checks": entry["checks"]})
    return {"status": "passed", "provenance": {"commit": commit, "source_sha256": source_sha256, "run_platform": run_platform, "host_run": bool(host_run), "project": data["project"], "working_tree_dirty": bool(dirty), "evidence_class": data["evidence_class"]}, "entries": result}


def collect_evidence(report, output, root=ROOT):
    root = Path(root)
    output = Path(output)
    manifest = load_manifest(root)
    structure = check_structure(manifest)
    check_deliveries(manifest, root)
    rows = check_profiles(manifest, root)
    versions = check_versions(manifest, root)
    schemas = check_event_schemas(manifest, root)
    check_critical(manifest, root)
    assets = check_assets(manifest, root)
    mappings = load_mappings(root)
    groups = m0.load(root / "tests/contract/subtests.json")["groups"]
    closure = check_report_closure(report, mappings, groups)
    regressions = validate_regression_logs(report, output)
    commands = {command["name"]: command for command in report["commands"]}
    if commands.get("template-source", {}).get("exit_code") != 0:
        raise ValueError("same-version template bundle gate is missing or failed")
    templates = m0.load(output / "templates.json")
    if templates.get("status") != "passed" or {profile["profile"] for profile in templates.get("profiles", [])} != {"tenant-only", "organization-enabled"}:
        raise ValueError("generated profile evidence is incomplete")
    coverage = release_coverage(m0.load(output / "coverage.json"), manifest["critical_packages"])
    drills = m0.load(output / "release-drills" / "release-drills.json")
    release_drills = report.get("release_drills", {})
    drills_summary = validate_drills(drills, output / "release-drills", report["commit"], report["source_sha256"],
                                     report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"],
                                     project=release_drills.get("project"), host_run=release_drills.get("host_run", False),
                                     dirty=report.get("working_tree_dirty", False))
    return {
        "status": "passed", "baseline": BASELINE, "commit": report["commit"], "source_sha256": report["source_sha256"],
        "platform": report["platform"]["GOOS"] + "-" + report["platform"]["GOARCH"],
        "product_version": manifest["product_version"], "version_scope": manifest["version_scope"],
        "versions": versions, "event_schemas": manifest["event_schemas"]["entries"],
        "requirements": structure["requirements"], "subtests": structure["subtests"],
        "deliveries": manifest["deliveries"], "profiles": rows, "closure": closure,
        "regressions": regressions, "template_bundle": "verified",
        "template_profiles": sorted({profile["profile"] for profile in templates["profiles"]}),
        "coverage": coverage, "drills": drills_summary, "assets": assets,
        "manifest_sha256": m0.digest(root / MANIFEST_REL), "gates": list(GATES),
    }


def build_evidence(report, output, root=ROOT):
    output = Path(output)
    evidence = collect_evidence(report, output, root)
    (output / "release-manifest.json").write_text(json.dumps(evidence, indent=2) + "\n", encoding="utf-8")
    return evidence


def validate_evidence(evidence, report, commit, directory, root=ROOT):
    if evidence != collect_evidence(report, Path(directory), root):
        raise ValueError("recorded release evidence cannot be reproduced from the artifacts")
    if evidence["commit"] != commit or report["commit"] != commit or evidence["source_sha256"] != report["source_sha256"]:
        raise ValueError("release evidence is not bound to the verified source commit")
    if evidence["status"] != "passed":
        raise ValueError("release evidence did not pass")


def check_manifest(manifest, root=ROOT):
    summary = check_structure(manifest)
    summary["deliveries"] = check_deliveries(manifest, root)
    summary["profiles"] = check_profiles(manifest, root)
    summary["versions"] = check_versions(manifest, root)
    summary["event_schemas"] = check_event_schemas(manifest, root)
    summary["critical_packages"] = check_critical(manifest, root)
    summary["assets"] = check_assets(manifest, root)
    return summary


def check_tenant_only(deps):
    for name in deps:
        if name.startswith(m0.MODULE + "/modules/organization"):
            raise ValueError("tenant-only assembly depends on the optional organization module: " + name)


def check(root=ROOT):
    root = Path(root)
    manifest = load_manifest(root)
    summary = check_manifest(manifest, root)
    summary["closure"] = check_closure(load_mappings(root), m0.load(root / "tests/contract/subtests.json")["groups"])
    return summary


def load_manifest(root=ROOT):
    manifest = m0.load(Path(root) / MANIFEST_REL)
    check_structure(manifest)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--root", default=str(ROOT))
    args = parser.parse_args()
    if not args.check:
        parser.error("specify --check to validate the committed release manifest")
    try:
        print(json.dumps(check(Path(args.root)), indent=2))
        return 0
    except (ValueError, OSError, KeyError) as error:
        print("release manifest check failed:", error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
