#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
exec python3 scripts/m5.py verify --project "${PANGOLIN_M5_PROJECT:?set the isolated M5 Compose project}" --race
