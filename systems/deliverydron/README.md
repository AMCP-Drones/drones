# Delivery drone system

Multi-component delivery drone system (same structure as agro in `cyber_drons`), with delivery-specific components: **cargo** instead of sprayer, and delivery mission semantics.

## Components

| Component         | Role                          | Implementation        |
|------------------|-------------------------------|------------------------|
| delivery_drone   | Main delivery logic, health   | Full (cmd/delivery_drone) |
| security_monitor | Policy / proxy (placeholder)  | Stub                   |
| journal          | Event log (placeholder)       | Stub                   |
| navigation       | Nav state (placeholder)      | Stub                   |
| mission_handler  | Delivery missions (placeholder) | Stub                |
| autopilot        | Control (placeholder)         | Stub                   |
| limiter          | Geofence (placeholder)       | Stub                   |
| emergency        | Emergency (placeholder)      | Stub                   |
| motors           | Motors/SITL (placeholder)    | Stub                   |
| cargo            | Cargo bay (placeholder)      | Stub                   |
| telemetry        | Telemetry (placeholder)      | Stub                   |

Stub components run a minimal broker subscriber (ping/get_status only) so the system composes and can be extended later.

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

Broker (Kafka or MQTT) is defined in repo root `docker/docker-compose.yml`. The prepare script merges it with this system’s services into `.generated/`. Use `BROKER_TYPE=kafka` (default) or `mqtt` when starting.