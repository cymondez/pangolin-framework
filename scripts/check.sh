#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
exec python3 scripts/m3.py verify --project "${PANGOLIN_M3_PROJECT:?set the isolated M3 Compose project}" --race
