"""Run the fixed Linux/amd64 M7 full-same-source gate on its pinned runner.

Reuses the shared m3_linux mechanism: a disposable exact-byte checkout of this
working tree is cloned inside the pinned OCI runner, then
``scripts/m7.py verify --project <project> --race --external-broker-fixture``
executes the whole cumulative gate (M0 inventory through M6 release
acceptance, plus the M7 realtime acceptance). The runner has no Docker
socket, so the host must run the five release drills against the same source
bytes first and pass the report via PANGOLIN_M6_DRILLS_DIR; the official
@microsoft/signalr client harness needs the local demo key material.
"""
import sys

import m3_linux

if __name__ == "__main__":
    raise SystemExit(m3_linux.main(milestone=7))
