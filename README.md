# Drones (delivery UAS)

Go services for a **multi-component delivery drone** that talk only over **Kafka or MQTT**: a policy-based **security monitor** (`proxy_request` / `proxy_publish`), **autopilot**, **mission handler**, **cargo**, **telemetry**, and related on-board components.

## Repository layout

| Path | What it is |
|------|------------|
| [`cmd/`](cmd/) | One `main` per component binary (`autopilot`, `security_monitor`, `delivery_drone`, …). |
| [`src/`](src/) | Shared code: [`bus`](src/bus) (Kafka/MQTT), [`config`](src/config), [`component`](src/component), and package-per-component logic. |
| [`systems/deliverydron/`](systems/deliverydron/) | Full-stack Compose for the delivery system; Dockerfiles live under `systems/deliverydron/src/<component>/docker/`. |
| [`docker/`](docker/) | Broker-only Compose (Kafka, Mosquitto); merged into `.generated` by prepare. |
| [`scripts/prepare_system.py`](scripts/prepare_system.py) | Merges broker + system Compose and env → `systems/deliverydron/.generated/`. |
| [`docs/`](docs/) | Architecture and integration documentation (see below). |
| [`tests/`](tests/) | Unit, module, and integration tests (`testutil` in-memory bus); optional Kafka e2e under `tests/e2e/`. |

Generated artifacts (do not edit by hand): `systems/deliverydron/.generated/docker-compose.yml` and `.env` — run `make prepare`.

## Documentation

| Doc | Contents |
|-----|----------|
| [**docs/quick_start.md**](docs/quick_start.md) | Prerequisites, `make` targets, broker profiles, tests overview. |
| [**docs/SYSTEM.md**](docs/SYSTEM.md) | Topics (`v1.<system>.<instance>.<component>`), components, security monitor, policies, flows. |
| [**docs/EXTERNAL_API.md**](docs/EXTERNAL_API.md) | Broker-level integration for ground/platform systems. |
| [**systems/deliverydron/README.md**](systems/deliverydron/README.md) | Component list and Docker-focused quick start. |
| [**docker/README.md**](docker/README.md) | Running the single `delivery_drone` image against an existing broker network. |
| [**tests/README.md**](tests/README.md) | Test layers and how to run e2e (`-tags=e2e`). |
| [`uas_architecture_spec.md`](uas_architecture_spec.md) | High-level UAS / safety-module specification (reference). |

## Common commands

```bash
make prepare      # generate .generated compose + env
make system-up    # broker + all deliverydron services
make system-down
make unit-test    # go test ./...
make test-e2e     # real Kafka when E2E_KAFKA=1 (see tests/README.md)
```

Default broker topic prefix is configurable via `TOPIC_VERSION`, `SYSTEM_NAME`, and `INSTANCE_ID` (see **docs/SYSTEM.md**).
