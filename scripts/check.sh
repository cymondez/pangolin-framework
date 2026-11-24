#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
exec python3 scripts/m6.py verify --project "${PANGOLIN_M6_PROJECT:?set the isolated M6 Compose project}" --race
