#!/usr/bin/env bash
# Shared setup for regulator security-goal test scripts.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export CGO_ENABLED=0
