#!/usr/bin/env bash
# FW-SEC-05 / CB-5: execution components only accept trusted operations.
set -euo pipefail
source "$(dirname "$0")/common.sh"
go test ./tests -count=1 -run 'TestModule_(Navigation_|Motors_|Cargo_)'
