#!/usr/bin/env bash
# FW-SEC-02 / CB-2: policy enforcement point (PEP) configuration.
set -euo pipefail
source "$(dirname "$0")/common.sh"
go test ./tests -count=1 -run 'TestModule_SecurityMonitor_(PolicyAdmin|ParsePolicies)|TestModule_SecurityMonitor_IsolationStart'
