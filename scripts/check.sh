#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
exec python3 scripts/m2.py verify --project "${PANGOLIN_M2_PROJECT:?set the isolated M2 Compose project}" --race
