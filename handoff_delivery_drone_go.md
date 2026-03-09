# Handoff: Delivery Drone Module (Go) — Summary for AI Agent

## Context

This repo (`sbd-drones-economics`) is a **platform** for modeling drone economics. Multiple teams develop **modules** (systems/components) in **separate git repos**. Modules communicate **only via a message broker** (Kafka or MQTT). One team is building the **delivery drone module in Go** and needs to integrate with this platform.

---

## Decisions Made in This Chat

1. **Separate repo**: Delivery drone is developed in its own git repo; integration with this repo is via **git submodule** (add submodule agreed with repo admin).
2. **Language**: Go is allowed (spec allows Python, Go, Rust). No need to use this repo’s Python SDK.
3. **Go “SDK”**: The team should implement in Go:
   - **Protocol**: Same message format as `sdk/messages.py` (action, payload, sender, correlation_id, reply_to, timestamp; response format with action "response", success, error).
   - **Broker abstraction**: One interface (publish, subscribe, request/response), two implementations (Kafka, MQTT), chosen via config so **broker can be switched on demand** (same as Python `bus_factory`).
   - **Optional**: Base component/system helpers (register handler by action, built-in ping/get_status) for consistency and less boilerplate.
4. **Docker**: Multi-stage Dockerfile (build in Go image, run in minimal image e.g. alpine). **No env in Dockerfile** — all broker/config comes from **environment in docker-compose**.
5. **Environment variables** the Go service must support (same as platform):
   - **Broker selection**: `BROKER_TYPE` (kafka | mqtt), `COMPONENT_ID` or `SYSTEM_ID`.
   - **Kafka**: `KAFKA_BOOTSTRAP_SERVERS`, `BROKER_USER`, `BROKER_PASSWORD`; optional `KAFKA_GROUP_ID`, fallback `KAFKA_HOST`/`KAFKA_PORT`.
   - **MQTT**: `MQTT_BROKER` or `MQTT_HOST`, `MQTT_PORT`, `BROKER_USER`, `BROKER_PASSWORD`; optional `MQTT_QOS`.
   - **Health**: `HEALTH_PORT` for HTTP health endpoint (platform expects it for systems).

---

## Instructions for the Implementing Agent

### Repo structure (delivery drone Go repo)

The delivery drone Go repo **must** follow a structure similar to the rest of the platform:

- **`src/`** — Go source code (e.g. `src/` or standard Go layout with `cmd/`, `internal/`, `pkg/` under repo root; ensure there is a clear place for application code).
- **`docker/`** — Dockerfile and any docker-related files (e.g. `docker/Dockerfile`). Compose for the delivery drone service can live here or be generated/integrated by the main repo’s `prepare_system` flow.
- **`Makefile`** — Targets for build, test, docker build/run (e.g. `build`, `test`, `docker-build`, `docker-up`, `unit-test`). Align with this repo’s Makefile conventions where applicable.
- **Unit tests** — In a `tests/` or `*_test.go` layout; runnable via `make unit-test` (or equivalent).
- **Entry point** — **`main.go`** at repo root or under `cmd/delivery_drone/main.go`, so the Docker image runs a single binary (e.g. `CMD ["/app/delivery-drone"]` or similar).

Example layout:

```
delivery-drone/
├── main.go              # or cmd/delivery_drone/main.go
├── src/                 # or cmd/, internal/, pkg/
├── docker/
│   └── Dockerfile
├── tests/               # unit tests
├── Makefile
├── go.mod
└── go.sum
```

Ensure the Dockerfile builds the binary from the chosen entry point and that `make unit-test` runs the Go unit tests.

### Reference implementation (Python, agriculture drone)

A **reference implementation** in Python by a neighboring team (agriculture drone) is available at:

**`/home/michael/Development/SPBU/cyber_drons`**

Use it to mirror:

- Project layout (src, docker, Makefile, tests, entry point).
- How the broker is used (topics, message format, request/response).
- How env vars (broker type, credentials, hosts, ports) are consumed.

The delivery drone Go module should be **structurally and behaviorally aligned** with that reference so it fits the same platform and tooling (e.g. `prepare_system`, docker-compose, e2e).

---

## Key Files in This Repo (sbd-drones-economics)

- **Message protocol**: `sdk/messages.py` — message/response shape.
- **Bus interface**: `broker/src/system_bus.py` — abstract SystemBus (publish, subscribe, request, etc.).
- **Broker factory**: `broker/src/bus_factory.py` — create Kafka or MQTT bus from `BROKER_TYPE` and env.
- **Broker config**: `broker/config.py` — env vars for Kafka/MQTT (hosts, ports).
- **Component example**: `components/dummy_component/` — structure, Dockerfile, handlers.
- **System example**: `systems/dummy_system/` — docker-compose env for components, network `drones_net`.
- **Docker env example**: `docker/example.env` — `BROKER_TYPE`, credentials, ports.
- **Compose**: `docker/docker-compose.yml` — broker services; `systems/dummy_system/docker-compose.yml` — component env pattern.

---

## Success Criteria for the Go Delivery Drone Module

- Repo has **src**, **docker**, **Makefile**, **unit tests**, and **main.go** (or equivalent) as entry point.
- Go SDK supports **Kafka and MQTT** via one interface, selected by **BROKER_TYPE** (and same env vars as above).
- Message format and request/response semantics match **sdk/messages.py** and **broker** behavior.
- Docker image runs one binary; connection/config via **environment** (no hardcoded credentials in Dockerfile).
- Structure and conventions are consistent with the **reference implementation** at `/home/michael/Development/SPBU/cyber_drons`.
