# Test suite

Tests live under `tests/` and use an in-memory **`testutil.MemoryBus`** (no broker) unless noted.

## Layers

| Layer | Location | What runs |
|-------|----------|-----------|
| **Unit** | `unit_*_test.go`, `tests/testutil/*_test.go` | Config, SDK message shape, `MemoryBus`, `component.IsTrustedSender` |
| **Module** | `module_*_test.go`, `component_test.go` | Single component + `MemoryBus` (journal, navigation, security monitor, delivery drone) |
| **Integration** | `integration_*_test.go` | Multiple components wired on one `MemoryBus` (cargo → monitor → journal, mission handler flow, telemetry polling) |
| **End-to-end** | `tests/e2e/` (`//go:build e2e`) | Real Kafka when `E2E_KAFKA=1` and broker env are set |

## Commands

```bash
# Default CI / local (no Kafka)
CGO_ENABLED=0 go test ./tests/... ./tests/testutil/... -count=1

# With e2e tag (broker must be up)
E2E_KAFKA=1 KAFKA_BOOTSTRAP_SERVERS=localhost:9092 \
  BROKER_USER=admin BROKER_PASSWORD=your_secret \
  CGO_ENABLED=0 go test -tags=e2e ./tests/e2e/... -v
```

`make unit-test` from the repo root runs `go test ./...` (includes these tests).

## Environment

Several tests call `t.Setenv` for `SECURITY_POLICIES`, `JOURNAL_FILE_PATH`, etc. Do not run those tests in parallel with other packages that depend on the same process env (the standard `go test` package isolation is per package; within `package tests`, tests run sequentially unless `t.Parallel()` is used — env-mutating tests here do not use `Parallel`).
