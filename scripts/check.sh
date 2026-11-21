#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
exec python3 scripts/m1.py verify --project "${PANGOLIN_M1_PROJECT:?set the isolated M1 Compose project}" --race
