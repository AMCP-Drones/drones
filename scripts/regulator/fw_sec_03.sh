#!/usr/bin/env bash
# FW-SEC-03 / CB-3: fail-safe deny and untrusted command rejection.
set -euo pipefail
source "$(dirname "$0")/common.sh"
go test ./tests -count=1 -run 'TestModule_(Motors_Trusted|Cargo_Open|Journal_Rejects)'
