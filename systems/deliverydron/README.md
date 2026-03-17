# Delivery drone system

Multi-component delivery drone system (same structure as agro in `cyber_drons`), with delivery-specific components: **cargo** instead of sprayer, and delivery mission semantics.

## Components

| Component         | Role                          | Implementation        |
|------------------|-------------------------------|------------------------|
| delivery_drone   | Main delivery logic, health   | Full (cmd/delivery_drone) |
| security_monitor | Policy gateway, proxy_request/proxy_publish, isolation | Full (cmd/security_monitor) |
| journal          | Append-only event log (LOG_EVENT, NDJSON) | Full (cmd/journal) |
| navigation       | Nav state (mock/SITL), get_state | Full (cmd/navigation) |
| mission_handler  | WPL/JSON missions, validate, send to autopilot | Full (cmd/mission_handler) |
| autopilot        | Control loop, motors + cargo  | Full (cmd/autopilot)   |
| limiter          | Geofence, limiter_event to emergency | Full (cmd/limiter) |
| emergency        | Emergency protocol (isolation, LAND, cargo close) | Full (cmd/emergency) |
| motors           | SET_TARGET, LAND, get_state, SITL commands | Full (cmd/motors) |
| cargo            | OPEN/CLOSE, get_state         | Full (cmd/cargo)      |
| telemetry        | Aggregate motors + cargo state | Full (cmd/telemetry)  |

All components use the shared bus (Kafka or MQTT) and follow the same patterns as the agrodron system (security_monitor policies, proxy_request/proxy_publish, journal LOG_EVENT).

## Quick start

From repo root:

1. **Vendor deps** (needed for Docker builds):  
   `make vendor`

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

## Broker

Broker (Kafka or MQTT) is defined in repo root `docker/docker-compose.yml`. The prepare script merges it with this system's services into `.generated/`. Use `BROKER_TYPE=kafka` (default) or `mqtt` when starting.