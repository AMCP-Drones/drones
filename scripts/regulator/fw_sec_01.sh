#!/usr/bin/env bash
# FW-SEC-01 / CB-1: access control and policy enforcement (security monitor).
set -euo pipefail
source "$(dirname "$0")/common.sh"
go test ./tests -count=1 -run 'TestModule_SecurityMonitor_(ProxyPublish|Isolation|Watchdog)'
