# Quick start

Go implementation of a multi-component **delivery drone** system on Kafka or MQTT: security monitor, policy-gated `proxy_request` / `proxy_publish`, and delivery-focused components (**cargo**, **delivery_drone**).

## Repository layout

| Path | Purpose |
|------|---------|
| `systems/deliverydron/<component>/cmd/<component>/` | `main` package for each on-board component binary |
| `systems/deliverydron/<component>/src/` | Go package code (shared crates like `bus`, `config`, `component` live under their own `systems/deliverydron/<name>/src/`) |
| `systems/deliverydron/` | Docker Compose for the full system; images build from `systems/deliverydron/<component>/docker/` |
| `docker/` | Broker stack (Kafka / Mosquitto) merged by `scripts/prepare_system.py` |
| `scripts/prepare_system.py` | Merges broker + system compose and builds `.generated/docker-compose.yml` and `.env` |
| `docs/` | System and API documentation |

## Prerequisites

- Go 1.21+ (for local builds)
- Docker / Docker Compose
- Python 3 + PyYAML (`pip install pyyaml`) for `prepare_system.py`

## One-time setup

From the repository root:

```bash
make vendor          # optional: vendored modules for offline Docker builds
make init            # ensure docker/.env exists (from example.env if missing)
make prepare         # writes systems/deliverydron/.generated/*
```

## Run the full stack

```bash
make system-up       # prepare + docker compose up in deliverydron
# or manually:
cd systems/deliverydron && make docker-up
```

Stop:

```bash
make system-down
```

Broker profile is controlled by `BROKER_TYPE` (`kafka` or `mqtt`) in the generated env file.

## Tests

```bash
make unit-test
# or
go test ./...
```

Layered tests (unit / module / integration) and optional Kafka e2e: see [tests/README.md](../tests/README.md).

## Documentation

- [SYSTEM.md](SYSTEM.md) — architecture, topics, components, security monitor, policies
- [EXTERNAL_API.md](EXTERNAL_API.md) — message shapes and integration notes for ground / platform systems

## Topic naming (summary)

Internal components use:

`{TOPIC_VERSION}.{SYSTEM_NAME}.{INSTANCE_ID}.{component}`

Defaults: `v1.deliverydron.Delivery001.<component>`. Override with `TOPIC_VERSION`, `SYSTEM_NAME`, `INSTANCE_ID`, or per-service `COMPONENT_TOPIC`. See [SYSTEM.md](SYSTEM.md) §2.
