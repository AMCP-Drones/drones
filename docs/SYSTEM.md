# Delivery drone system

## 1. Overview

This repository implements an on-board **delivery UAS** as a set of processes that communicate **only through a message broker** (Kafka or MQTT). Cross-component traffic is mediated by a **security monitor**: it enforces allow-lists of `(sender, target_topic, action)` and exposes `proxy_request` / `proxy_publish` so peers do not talk to each other directly without policy checks.

Delivery-specific choices in this stack include **cargo** bay control (`OPEN` / `CLOSE`) instead of a crop sprayer, an **`emergency`** component (not `emergensy`), and an optional **`delivery_drone`** service for platform-facing APIs and health checks alongside **`autopilot`**. External ATM / GCS / droneport dialogue on mission start is not implemented in `src/autopilot` today (extension point).

### 1.1 Topic format

Each internal component subscribes to a single primary topic:

```text
{TOPIC_VERSION}.{SYSTEM_NAME}.{INSTANCE_ID}.{component}
```

**Example:** `v1.deliverydron.Delivery001.autopilot`

Environment variables:

| Variable | Default | Role |
|----------|---------|------|
| `TOPIC_VERSION` | `v1` | Protocol / namespace version segment |
| `SYSTEM_NAME` | `deliverydron` | Logical system name |
| `INSTANCE_ID` | `Delivery001` | Fleet or airframe instance |
| `COMPONENT_ID` | varies | Process identity (often matches last segment) |
| `COMPONENT_TOPIC` | *(derived)* | Override full topic if you must use a legacy flat name |

Derived topic when `COMPONENT_TOPIC` is unset:

```text
${TOPIC_VERSION}.${SYSTEM_NAME}.${INSTANCE_ID}.${COMPONENT_ID}
```

### 1.2 Message envelope

JSON messages carry at least:

```json
{
  "action": "handler_name",
  "sender": "component_id_or_logical_sender",
  "payload": { }
}
```

For request/response, the bus adds `correlation_id` and `reply_to` (implementation detail of `src/bus`).

**Note:** This codebase mixes **lowercase** actions (`get_state`, `mission_load`, `limiter_event`) with **uppercase** actuator-style actions (`SET_TARGET`, `LAND`, `LOG_EVENT`, `LOAD_MISSION`, `OPEN`, `CLOSE`). Policies must match the exact string used by the producer.

Components that only accept commands from the security monitor check that `sender` begins with `security_monitor` (the monitor forwards with `sender` set to its `COMPONENT_ID`, typically `security_monitor`).

---

## 2. Component catalog

| Component | Typical `COMPONENT_ID` | Role |
|-----------|----------------------|------|
| `security_monitor` | `security_monitor` | Policy gateway: `proxy_request`, `proxy_publish`, isolation |
| `autopilot` | `autopilot` | Mission state machine, navigation polling, motors/cargo commands via proxy |
| `mission_handler` | `mission_handler` | WPL / JSON missions, `LOAD_MISSION`, `VALIDATE_ONLY`, pushes `mission_load` to autopilot & limiter |
| `navigation` | `navigation` | Holds NAV state; `get_state`, `nav_state`, `update_config` |
| `motors` | `motors` | `SET_TARGET`, `LAND`, `get_state`; optional SITL on `SITL_COMMANDS_TOPIC` (default `sitl.commands`) |
| `cargo` | `cargo` | `OPEN`, `CLOSE`, `get_state` |
| `limiter` | `limiter` | Geofence / deviation; `mission_load`, polls nav & telemetry; may signal `emergency` |
| `emergency` | `emergency` | `limiter_event` → isolation + safe state (cargo close, motors land, journal) |
| `telemetry` | `telemetry` | Aggregates motors + cargo via proxy |
| `journal` | `journal` | Append-only `LOG_EVENT` to NDJSON file |
| `delivery_drone` | `delivery_drone` | Platform-facing binary (`cmd/delivery_drone`): health HTTP, broker handlers (`echo`, `deliver_package`, …) |

---

## 3. Security monitor

Topic: `…security_monitor` (full hierarchical name).

### 3.1 Client usage

1. Publish `proxy_request` or `proxy_publish` **to** the security monitor topic.
2. Payload includes `target.topic` (full string) and `target.action`.
3. Monitor checks `(message.sender, target.topic, target.action)` against the policy set.
4. On allow, it forwards to `target.topic` with `sender` set to the monitor’s `COMPONENT_ID` (so downstream components see a trusted sender prefix `security_monitor`).

### 3.2 Monitor actions

| Action | Purpose |
|--------|---------|
| `proxy_request` | RPC-style forward; wraps response in `target_topic` / `target_action` / `target_response` |
| `proxy_publish` | Fire-and-forget forward |
| `set_policy` / `remove_policy` / `clear_policies` / `list_policies` | Admin policy CRUD (`POLICY_ADMIN_SENDER` must match `sender`) |
| `ISOLATION_START` | Emergency isolation: replace policies with a minimal emergency set |
| `isolation_status` | Returns `NORMAL` or `ISOLATED` |

### 3.3 Policies

Policies are a JSON array of objects `{ "sender", "topic", "action" }` in `SECURITY_POLICIES` (passed into the security_monitor container). **Sender** is the `sender` field on the **incoming** message to the monitor (usually the origin component’s `COMPONENT_ID`, e.g. `autopilot`). **Topic** is the **full** broker topic of the callee.

To avoid duplicating the `v1.deliverydron.Delivery001` prefix in every row, `scripts/prepare_system.py` expands:

- `${SYSTEM_NAME}` and `${TOPIC_PREFIX}` → `{TOPIC_VERSION}.{SYSTEM_NAME}.{INSTANCE_ID}`

So a policy row can use `"topic": "${SYSTEM_NAME}.navigation"` which becomes `v1.deliverydron.Delivery001.navigation`.

At runtime, the security monitor performs the same substitution from its config so hand-written env files stay consistent.

---

## 4. Representative flows

### 4.1 Load mission (platform → mission_handler)

Platform uses `sender` `platform` (must match policy) and either:

- `proxy_request` / `proxy_publish` through the security monitor to `…mission_handler` with action `LOAD_MISSION` or `VALIDATE_ONLY`, or  
- Direct publish if your deployment allows (not the default trusted path for internal components).

Mission handler then uses the monitor to call `mission_load` on `autopilot` and `limiter`.

### 4.2 Execute mission

Autopilot polls navigation via `proxy_request`, commands motors with `SET_TARGET`, and cargo with `OPEN` / `CLOSE` via `proxy_publish`, each allowed by policy.

### 4.3 Emergency

Limiter detects a breach and can publish to `…emergency` (direct broker publish with `sender` = `limiter`). Emergency then publishes `ISOLATION_START` and `proxy_publish` chains on the monitor topic with `sender` = `emergency`.

---

## 5. Configuration and code generation

| File | Purpose |
|------|---------|
| `docker/example.env` | Broker + default `TOPIC_VERSION` / `SYSTEM_NAME` / `INSTANCE_ID` |
| `systems/deliverydron/deliverydron.env` | Committed system-wide topic defaults (`TOPIC_VERSION`, `SYSTEM_NAME`, `INSTANCE_ID`) |
| `systems/deliverydron/.env` | Optional local overrides (gitignored filename) |
| `systems/deliverydron/src/<component>/.env` or `<component>.env` | Per-component vars; merged as `<PREFIX>_<KEY>` in `.generated/.env` |
| `systems/deliverydron/.generated/*` | **Generated** — run `make prepare` |

```bash
make prepare
```

---

## 6. Simulator / SITL

Motor commands default to a **flat** topic `sitl.commands` (typical for a simulator command bus). Override with `SITL_COMMANDS_TOPIC`.

---

## 7. Implementation notes

- **Language / runtime:** Go; broker adapters in `src/bus` (Kafka and MQTT).
- **Topics:** Hierarchical defaults `v1.deliverydron.Delivery001.<component>`; override per deployment via env.
- **External ATM / GCS integration:** Extension points in [EXTERNAL_API.md](EXTERNAL_API.md); not wired in `src/autopilot` today.
- **Action naming:** Mixed case (`SET_TARGET`, `LOAD_MISSION`, `get_state`, …). Policies must use the exact action strings this codebase emits.

---

## 8. Project structure (system)

```text
systems/deliverydron/
  docker-compose.yml       # Component services (broker merged by prepare)
  .env                     # Optional system-wide topic defaults
  .generated/              # Output of prepare_system.py
  src/
    <component>/docker/Dockerfile
    security_monitor/security_monitor.env  # Default SECURITY_POLICIES with ${SYSTEM_NAME} placeholders
```

Root `Makefile` targets: `prepare`, `system-up`, `system-down`, `unit-test`, `vendor`.
