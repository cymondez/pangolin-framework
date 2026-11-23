#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
exec python3 scripts/m4.py verify --project "${PANGOLIN_M4_PROJECT:?set the isolated M4 Compose project}" --race
