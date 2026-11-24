# M6 evidence index — worker results (GLM Flash)

Deliverables: `scripts/m6_index.py`, `tests/m6/test_index.py`. No other files touched (read-only on plan/contracts/`release_manifest.py`/`m6.py`). No commits, no services, no Docusaurus edits.

## Exact integration API

```python
import m6_index  # scripts/m6_index.py; importable: imports m0, m1, release_manifest only

index = m6_index.build_index(report, directory, root=m6_index.ROOT)  # -> dict; raises ValueError on any unprovable mapped case
m6_index.write_index(report, directory, root=m6_index.ROOT)          # build + write directory/profile-evidence.json (indent=2, sort_keys), returns same dict
```

- `report` = the in-memory verification report dict (the same object `m1.verify` mutates); `directory` = the report output directory (Path or str); both accept `Path`/str.
- Lead integration point: call from the M6 `extra_steps` hook (`m6.py release_steps`, after `release_manifest.build_evidence`) as `m6_index.write_index(report, output)`. It is safe **before** final `report["artifacts"]` hashing: artifacts dict may be absent — the builder hashes referenced files directly from disk and binds them to `command["sha256"]`; an artifacts dict, when populated (also with Windows `\` keys), is cross-checked and mismatches are rejected. Because it is written before the lead's final `rglob` hash collection, `profile-evidence.json` becomes an ordinary bound artifact; `m6.py validate_report` may then add `"profile-evidence.json"` to its required raw names list and optionally bind `report["artifacts"]["profile-evidence.json"]`.
- Module patchable seams (used by tests only): `_mappings(root)`, `_subtest_groups(root)`, `_profiles(root, groups)`, constant `NAMED_SUBTESTS` (=421).

## Index object shape

```json
{
  "spec": "13.6", "kind": "release-evidence-index", "baseline": "v1.3",
  "commit": "<40-hex>", "source_sha256": "<64-hex>",
  "evidence_class": "evidence-ready | development-only",   // never "verified"
  "report_status": "<as provided>", "platform": "windows-amd64 | null",
  "closure": {"mapped": 421, "milestones": {"M1": 47, "M2": 133, "M3": 185, "M4": 18, "M5": 25, "M6": 13}},
  "totals": {"subtests": 421, "rows": 1263, "required_rows": 1185, "not_applicable_rows": 78},
  "profiles": {"<name>": {"manifest_sha256": "...", "required": n, "not_applicable": n}},  // sorted names, manifests read-only
  "contract_inputs": {"tests/contract/m1.json": "...", ..., "tests/contract/subtests.json": "..."},
  "rows": [{
    "subtest": "A01/x", "requirement": "MOD-001", "milestone": "M1",
    "mapping": {"field": "go_tests|unit_tests|integration_tests|python_tests|template_tests|drills", "identity": "<mapped test name>"},
    "profiles": {"<name>": {"applicability": "required|not-applicable", "reason": "<verbatim manifest reason>",
                            "evidence": {proof} | null}}
  }]
}
```

Proof objects (only for required rows; every mapped case of all 421 subtests is proven once per report):

- go (`go_tests`/`unit_tests`): `{"kind":"go-test","command":"unit","exit_code":0,"log":"<NN-unit.log>","sha256":...,"identity":"<pkg:Test>"}`
- integration: `{"kind":"integration-test","command":"integration",...same fields}`
- python (`python_tests`): `{"kind":"python-regression","command":"mN-regression",...,"identity":"<unittest name>"}`
- template: `{"kind":"template","contract":"both-profiles-build-test-start","command":"templates","artifact":"templates.json","sha256":...,"build_test_start_profiles":["organization-enabled","tenant-only"]}`
- drill: `{"kind":"drill","entry":"clean-rebuild","entry_status":"passed","artifact":"release-drills/release-drills.json","sha256":...,"provenance":{"commit":...,"source_sha256":...}}`
- Shared contract evidence is marked **in** the evidence dict: `"shared_contract": true` plus `"linked_profiles": [...]` when several profile rows share the same mapped contract proof (not separate test runs).

## Positive-proof rules (fail-closed)

1. Go unit/integration: exactly one command with the mapped name, `exit_code==0`; log path normalized (`\`→`/`), refused on `:`, absolute or `..` escape; direct file sha256 must equal `command["sha256"]` and, if present, `artifacts[log]`; content validated by `m1.validate_tests(text, (identity,))` — fails on fail/skip events or missing required case. Rejects missing/failed/changed/skipped logs.
2. Python: exactly one `mN-regression` command (exit 0) with the same hash/containment checks; content validated by `m1.validate_python_output(text, (identity,))` — rejects zero/skipped/short runs and missing case names; evidence refers to the command log hash.
3. Template: `templates.json` must record `status=="passed"` and `build_test_start==True` for both `tenant-only` and `organization-enabled`; only the currently mapped `both-profiles-build-test-start` value is accepted (anything else raises); the `templates` command (exit 0, hash-bound) must exist.
4. Drills: `release-drills/release-drills.json` must contain the named entry with `status=="passed"` and its top-level `commit`/`source_sha256` must equal the report's; file hashed directly and cross-checked against artifacts. Schema depth stays with `release_manifest.validate_drills` (release gate), per packet.
5. Closure: `release_manifest.check_closure(mappings, groups)` proves exact unique 421 mapped IDs, correct milestone per ID, no blank/unknown/duplicate; `m0.check_profiles(groups, manifests)` validates the three profile plans (status `planned`, baseline `v1.3`, matching requirement + evidence template, non-empty reasons) — profile manifests are never modified.
6. Index refuses non-40-hex commit, non-64-hex source hash, or baseline ≠ `v1.3`; dirty (`working_tree_dirty` true) or `gate=="development-only"` reports → `evidence_class="development-only"`; clean Linux reports without a successful race command also degrade to `development-only`; `verified` is never produced.

## Tests (`tests/m6/test_index.py`, 14 cases, all passing)

Run: `python -m unittest discover -s tests/m6 -p test_index.py -v` → `Ran 14 tests ... OK`.

- `IndexTestCase` (temp-dir fixture, patched loaders + `NAMED_SUBTESTS=5`; explicit tiny mappings with one go pair + one python + one template + one drill; three profiles incl. one tenant-only N/A; raw JSONL/regression/templates.json/drill artifacts; artifacts dict deliberately `{}`):
  - raw-proof identity/linking + `write_index` round-trip; artifacts dict absent AND populated (incl. backslash key); missing log file; missing required case (hash re-synced so only the content check fires); changed log (hash drift); skipped test event; duplicate mapping across milestones; N/A row preserves reason verbatim with `evidence: null` and no pass claim; development reports never labelled `verified`; log path `..\` escape; drill provenance/status binding; unknown template contract rejected.
- `ClosureTestCase` (real production inputs, no patches): exact 421 unique via `check_closure` with milestone counts {M1:47, M2:133, M3:185, M4:18, M5:25, M6:13}; builder rejects a duplicate production mapping. `tests/m6/test_gate.py` untouched.

## Limits / notes

- Single `build_index` call covers one report directory (one platform); the three profile rows come from the committed profile manifests, so per-profile `tenant-only` / `organization-enabled` / `replacement-contract` rows share the same raw command proof for common contract tests — marked via `shared_contract`. The lead merges platform reports by `platform` field per spec 13.6.
- `contract_inputs` digests and `profiles[*].manifest_sha256` always hash the real contract files (fixture tests do not assert them).
- Windows paths: forward-slash normalized values inside the index; containment via `Path.is_relative_to` after `resolve()`; no secrets read; no global state.

Verification performed: 14/14 unittest OK (fixtures only, no services); production smoke — `check_closure` mapped=421, `_profiles` returns the three 421-row manifests. Full M6 two-platform gate evidence remains the lead's task.
