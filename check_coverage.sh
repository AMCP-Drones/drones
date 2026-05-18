#!/usr/bin/env bash
# Prints total line coverage percent for regulator CI (stdout: single number).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"
export CGO_ENABLED=0
PROFILE="$(mktemp)"
trap 'rm -f "$PROFILE"' EXIT
go test ./... -count=1 -coverprofile="$PROFILE" >/dev/null
go tool cover -func="$PROFILE" | awk '/^total:/ { gsub("%","",$3); print $3; exit }'
