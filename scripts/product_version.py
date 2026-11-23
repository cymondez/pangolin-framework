"""Framework product version policy; distinct from tool/schema/document versions."""
from pathlib import Path
import json
import re

ROOT = Path(__file__).resolve().parents[1]


def read(root=ROOT):
    version = (root / "VERSION").read_text(encoding="utf-8-sig").strip()
    if version != "0.1.0":
        raise ValueError("M1-M6 framework product version must be 0.1.0")
    return version


def check(root=ROOT):
    version = read(root)
    source = (root / "examples/bookstore/bootstrap/run.go").read_text(encoding="utf-8-sig")
    declared = re.findall(r'\bVersion:\s*"([^"]+)"', source)
    if not declared or any(value != version for value in declared):
        raise ValueError("framework-owned Bookstore module versions differ from VERSION")
    cli=root/'internal/scaffold/cli.go'
    if cli.exists():
        match=re.search(r'const Version\s*=\s*"([^"]+)"',cli.read_text(encoding='utf-8-sig'))
        if not match or match[1]!=version:raise ValueError('CLI/runtime product version differs from VERSION')
    fixture = json.loads((root / "tests/fixtures/modules.json").read_text(encoding="utf-8-sig"))
    if any(module["version"] != version for module in fixture["modules"]):
        raise ValueError("baseline module fixture versions differ from VERSION")
    site = root / "docs/site"
    if (site / "package.json").exists():
        package = json.loads((site / "package.json").read_text(encoding="utf-8-sig"))
        lock = json.loads((site / "package-lock.json").read_text(encoding="utf-8-sig"))
        if package["version"] != version or lock["version"] != version or lock["packages"][""]["version"] != version:
            raise ValueError("docs package/lockfile versions differ from VERSION")
    return version
