"""Exact module and reviewed license allowlist; a new dependency fails closed."""
from pathlib import Path
import m0


def inventory():
    result = []
    for module in m0.json_stream(m0.command(["go","list","-m","-json","all"])):
        if module.get("Main"): continue
        if module.get("Replace"): raise ValueError("unreviewed module replacement")
        if not module.get("Dir"):raise ValueError("dependency source unavailable; run go mod download all: "+module["Path"])
        directory = Path(module["Dir"])
        candidates = [directory / name for name in ("LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "COPYING")]
        license_path = next((p for p in candidates if p.is_file()), None)
        if license_path is None: raise ValueError("license missing: " + module["Path"])
        result.append({"module":module["Path"],"version":module["Version"],"sum":module.get("Sum"),"license_file":license_path.name,"license_sha256":m0.digest(license_path)})
    return result


def check():
    expected = m0.load(m0.ROOT / "deploy/dependencies.json")
    actual = inventory()
    validate(expected,actual)
    return {"locked_modules":len(actual),"licenses":"reviewed-allowlist"}


def validate(expected,actual):
    approved = {"MIT","MIT-0","MPL-2.0","Apache-2.0","MIT AND Apache-2.0","BSD-3-Clause AND Apache-2.0 AND MIT","BSD-3-Clause","BSD-2-Clause","ISC"}
    if len(expected["modules"]) != len(actual): raise ValueError("dependency inventory changed")
    for want, found in zip(expected["modules"], actual):
        if want.get("license") not in approved or {k:v for k,v in want.items() if k != "license"} != found:
            raise ValueError("dependency/license review required: " + found["module"])
