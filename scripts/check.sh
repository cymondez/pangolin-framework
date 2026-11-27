#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
project="${PANGOLIN_PROJECT:-${PANGOLIN_M6_PROJECT:?set the isolated Compose project}}"
scope="${PANGOLIN_SCOPE:-release}"
exec python3 scripts/verify.py verify --project "$project" --scope "$scope" --race
