"""Reviewed npm dependency/license inventory for the official M7 SignalR client.

The official client (examples/realtime/client) pins @microsoft/signalr 10.0.0
and `npm ci --ignore-scripts` resolves a fixed transitive tree. This validator
derives the exact installed package names, versions, resolved URLs, integrity
hashes and license-file digests from package-lock.json plus the installed
node_modules, then fails closed against the reviewed immutable manifest
deploy/release/m7-client-dependencies.json: a missing or extra dependency,
missing integrity, installed version mismatch, license-file content drift,
lock drift or an unreviewed SPDX license identifier all raise. License fields
are published registry metadata only; no legal claims are made by this check.
"""
import argparse
import json
from pathlib import Path
import sys

import m0

ROOT = m0.ROOT
CLIENT_REL = "examples/realtime/client"
MANIFEST_REL = "deploy/release/m7-client-dependencies.json"
LICENSE_FILES = ("LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "LICENCE.txt", "LICENCE.md", "COPYING", "UNLICENSE", "LICENSE-MIT.txt")
ENTRY_FIELDS = ("version", "resolved", "integrity", "license", "license_file", "license_sha256")
DRIFT = {"version": "lockfile version drift", "resolved": "resolved URL drift", "integrity": "integrity drift",
         "license": "license metadata drift", "license_file": "license-file content drift", "license_sha256": "license-file content drift"}


def inventory(client_root=None):
    """Derive the actual dependency inventory from the lockfile and the installed tree."""
    client_root = Path(client_root) if client_root else ROOT / CLIENT_REL
    lockfile = client_root / "package-lock.json"
    lock = m0.load(lockfile)
    packages = lock.get("packages")
    if not isinstance(packages, dict):
        raise ValueError("unsupported package-lock.json layout: " + str(lockfile))
    try:
        source = client_root.relative_to(ROOT).as_posix()
    except ValueError:
        source = str(client_root)
    result = {"client_root": source,
              "sources": {"package_json": source + "/package.json", "lockfile": source + "/package-lock.json"},
              "package_json_sha256": m0.digest(client_root / "package.json"),
              "lockfile_sha256": m0.digest(lockfile),
              "packages": {}}
    for key, entry in sorted(packages.items()):
        if not key or not key.startswith("node_modules/"):
            continue
        name = key[len("node_modules/"):]
        if not entry.get("version"):
            raise ValueError("lockfile entry missing version: " + name)
        if not entry.get("resolved"):
            raise ValueError("lockfile entry missing resolved URL: " + name)
        if not entry.get("integrity"):
            raise ValueError("missing integrity: " + name)
        installed = client_root / key / "package.json"
        if not installed.is_file():
            raise ValueError("installed package missing: " + name + "; run npm ci --ignore-scripts")
        metadata = m0.load(installed)
        if metadata.get("name") != name:
            raise ValueError("installed package name drift: " + name)
        if metadata.get("version") != entry["version"]:
            raise ValueError("installed version mismatch: " + name + " installs " + str(metadata.get("version")) + " but lock pins " + entry["version"])
        if metadata.get("license") != entry.get("license"):
            raise ValueError("installed license metadata mismatch: " + name)
        license_path = next((client_root / key / candidate for candidate in LICENSE_FILES if (client_root / key / candidate).is_file()), None)
        result["packages"][name] = {"version": entry["version"], "installed_version": metadata["version"],
                                    "resolved": entry["resolved"], "integrity": entry["integrity"],
                                    "license": entry.get("license"),
                                    "license_file": license_path.name if license_path else None,
                                    "license_sha256": m0.digest(license_path) if license_path else None}
    return result


def validate(expected, actual):
    """Fail closed unless the actual inventory is exactly the reviewed manifest."""
    for field in ("package_json_sha256", "lockfile_sha256"):
        if expected.get(field) != actual.get(field):
            raise ValueError("lock drift: package.json/package-lock.json no longer match the reviewed manifest")
    missing = sorted(set(expected["packages"]) - set(actual["packages"]))
    unexpected = sorted(set(actual["packages"]) - set(expected["packages"]))
    if missing:
        raise ValueError("dependency inventory changed; review required, missing: " + ", ".join(missing))
    if unexpected:
        raise ValueError("dependency inventory changed; review required, unexpected: " + ", ".join(unexpected))
    for name in sorted(expected["packages"]):
        want, found = expected["packages"][name], actual["packages"][name]
        if found["license"] not in expected.get("allowed_licenses", ()):
            raise ValueError("unknown license " + repr(found["license"]) + " for " + name)
        if want.get("license") not in expected.get("allowed_licenses", ()):
            raise ValueError("unreviewed license " + repr(want.get("license")) + " in manifest for " + name)
        if found["installed_version"] != found["version"]:
            raise ValueError("installed version mismatch: " + name + " installs " + found["installed_version"] + " but lock pins " + found["version"])
        for field in ENTRY_FIELDS:
            if want.get(field) != found.get(field):
                raise ValueError((DRIFT.get(field) or field) + ": " + name)


def check(client_root=None, manifest_path=None):
    """Validate the actual inventory against the reviewed manifest; returns a summary."""
    manifest = m0.load(Path(manifest_path) if manifest_path else ROOT / MANIFEST_REL)
    actual = inventory(client_root)
    validate(manifest, actual)
    return {"status": "ok", "locked_packages": len(actual["packages"]), "client_root": actual["client_root"],
            "package_json_sha256": actual["package_json_sha256"], "lockfile_sha256": actual["lockfile_sha256"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="validate the actual inventory against the reviewed manifest")
    parser.add_argument("--output", help="write the actual inventory JSON to this path (gate artifact)")
    parser.add_argument("--client-root", default=str(ROOT / CLIENT_REL))
    parser.add_argument("--manifest", default=str(ROOT / MANIFEST_REL))
    args = parser.parse_args()
    try:
        actual = inventory(args.client_root)
        if args.output:
            Path(args.output).write_text(json.dumps(actual, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        if args.check:
            print(json.dumps(check(args.client_root, args.manifest), indent=2, sort_keys=True))
        elif not args.output:
            print(json.dumps(actual, indent=2, sort_keys=True))
        return 0
    except (ValueError, OSError, KeyError, json.JSONDecodeError) as error:
        print("client dependency check failed:", error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
