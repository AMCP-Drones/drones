#!/usr/bin/env bash
# SYS-SEC-01 / CB-4: security audit journal.
set -euo pipefail
source "$(dirname "$0")/common.sh"
go test ./tests -count=1 -run 'TestModule_Journal_'
go test ./journal/... -count=1
