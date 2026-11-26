# M7 python gate tests

`test_gate.py` holds the service-free fail-closed regressions for the
acceptance runner (`scripts/m7.py`) and the additive per-profile evidence
index (`scripts/m7_index.py`): missing or skipped evidence, unknown source
bytes, insufficient coverage, unknown template contracts, drifted manifests,
pending acceptance and forged metadata must all raise. Real runtime acceptance
is executed by `python scripts/m7.py verify --project pangolin-m7-*` (full
cumulative gate) and `python scripts/m7.py gate --commit <full SHA>`.
