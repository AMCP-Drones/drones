# Delivery drone system

Multi-component delivery drone system on a shared broker, with delivery-specific components (**cargo**, **delivery_drone**) and delivery mission semantics.

**Documentation (English):** see the repo root [`docs/`](../../docs/) — [`SYSTEM.md`](../../docs/SYSTEM.md), [`EXTERNAL_API.md`](../../docs/EXTERNAL_API.md), [`quick_start.md`](../../docs/quick_start.md).

**Broker topics:** internal components support two schemes:
- legacy (default): `v1.deliverydron.Delivery001.<component>` via `TOPIC_VERSION`, `SYSTEM_NAME`, `INSTANCE_ID`
- monorepo-compatible: `components.<system>.<component>` via `TOPIC_SCHEME=components`
- explicit override: set `TOPIC_PREFIX=<prefix-without-component>`

## Components

| Component         | Role                          | Implementation        |
|------------------|-------------------------------|------------------------|
| delivery_drone   | Main delivery logic, health   | Full (`systems/deliverydron/delivery_drone/cmd/delivery_drone`) |
| security_monitor | Policy gateway, proxy_request/proxy_publish, isolation | Full (`systems/deliverydron/security_monitor/cmd/security_monitor`) |
| journal          | Append-only event log (LOG_EVENT, NDJSON) | Full (`systems/deliverydron/journal/cmd/journal`) |
| navigation       | Nav state (mock/SITL), get_state | Full (`systems/deliverydron/navigation/cmd/navigation`) |
| mission_handler  | WPL/JSON missions, validate, send to autopilot | Full (`systems/deliverydron/mission_handler/cmd/mission_handler`) |
| autopilot        | Control loop, motors + cargo  | Full (`systems/deliverydron/autopilot/cmd/autopilot`)   |
| limiter          | Geofence, limiter_event to emergency | Full (`systems/deliverydron/limiter/cmd/limiter`) |
| emergency        | Emergency protocol (isolation, LAND, cargo close) | Full (`systems/deliverydron/emergency/cmd/emergency`) |
| motors           | SET_TARGET, LAND, get_state, SITL commands | Full (`systems/deliverydron/motors/cmd/motors`) |
| cargo            | OPEN/CLOSE, get_state         | Full (`systems/deliverydron/cargo/cmd/cargo`)      |
| telemetry        | Aggregate motors + cargo state | Full (`systems/deliverydron/telemetry/cmd/telemetry`)  |

All components use the shared bus (Kafka or MQTT) with the security_monitor policy gateway (`proxy_request` / `proxy_publish`, isolation) and journal `LOG_EVENT`.

## Quick start

From repo root:

1. **Vendor deps (required preflight)**:  
   run `go mod vendor` from the repo root before Docker builds (or use `make preflight-vendor` in this system directory).

2. **Prepare** (generate `.generated/docker-compose.yml` and `.env`):  
   `make prepare`  
   (Requires Python 3 and PyYAML: `pip install -r scripts/requirements.txt` or use system package.)

3. **Start system** (broker + all components):  
   `make system-up`

4. **Stop**:  
   `make system-down`

Or from this directory:

- `make prepare` — generate merged compose and env
- `make docker-up` — start (prepare + compose up)
- `make docker-down` — stop
- `make docker-logs` — follow logs
- `make unit-test` — run Go tests from repo root

## Analytics Integration (DroneAnalytics)

`journal` and `telemetry` can optionally export data to DroneAnalytics:

- `ANALYTICS_ENABLED=true`
- `ANALYTICS_BASE_URL=https://infopanel.csse.ru/api`
- `ANALYTICS_API_KEY=<api-key>`
- `ANALYTICS_TIMEOUT_S=2.0`
- `ANALYTICS_API_VERSION=1.1.0`

Telemetry payload mapping:

- endpoint: `POST /log/telemetry`
- source: `telemetry` component (`last_target` from motors state)
- fields: `timestamp`, `drone`, `drone_id`, `latitude`, `longitude`, optional `height/course/battery`

Event payload mapping:

- endpoint: `POST /log/event`
- source: `journal` component (`LOG_EVENT`)
- fields: `timestamp`, `service`, `service_id`, `message`, optional `severity`, `event_type`

By default analytics export is disabled, so existing docker/runtime behavior is unchanged.

## Broker

Broker (Kafka or MQTT) is defined in repo root `docker/docker-compose.yml`. The prepare script merges it with this system's services into `.generated/`. Use `BROKER_TYPE=kafka` (default) or `mqtt` when starting.