# External integration API

This document describes how **external** systems (ground control, operations platform, simulators) interact with the delivery drone **at the broker layer**. Internal components use the same JSON envelope unless noted.

For full architecture, see [SYSTEM.md](SYSTEM.md).

---

## 1. Topic layout

### 1.1 On-board components

```text
{TOPIC_VERSION}.{SYSTEM_NAME}.{INSTANCE_ID}.{component}
```

Example defaults:

```text
v1.deliverydron.Delivery001.mission_handler
v1.deliverydron.Delivery001.autopilot
v1.deliverydron.Delivery001.security_monitor
```

### 1.2 Recommended external topics (convention)

If you model ground systems with the same hierarchical topic style as on-board components, you might use:

| Role | Example topic | Env placeholder |
|------|---------------|-----------------|
| Ground / platform “main” | `v1.Platform.Platform001.main` | *(define in your compose)* |
| NUS / GCS | `v1.NUS.NUS001.main` | `NUS_TOPIC` *(optional future use)* |
| ATM / ORVD | `v1.ORVD.ORVD001.main` | `ORVD_TOPIC` |
| Droneport | `v1.Droneport.DP001.main` | `DRONEPORT_TOPIC` |

This Go delivery stack **does not** currently implement ORVD / NUS / droneport dialogue in `autopilot`; those environment variables are reserved for `prepare_system.py` policy substitution and future extensions.

### 1.3 Simulator

- **Motor commands:** default Kafka/MQTT topic `sitl.commands` unless `SITL_COMMANDS_TOPIC` is set (flat name, not under `v1.…` by default).

---

## 2. Message envelope

```json
{
  "action": "ACTION_NAME",
  "sender": "logical_or_component_sender",
  "payload": { }
}
```

Responses (when using request/response) follow the shared response shape from `src/sdk` (`success`, `payload`, `correlation_id`, etc.).

---

## 3. Security monitor as the front door

External systems should **not** publish directly to internal component topics unless policies explicitly allow their `sender` and you accept the risk of bypassing the monitor.

**Preferred pattern:**

1. Publish to `…security_monitor` with `action`: `proxy_request` or `proxy_publish`.
2. Set `sender` to an id that appears in policies (e.g. `platform`).
3. Put the real target in `payload`:

```json
{
  "action": "proxy_request",
  "sender": "platform",
  "payload": {
    "target": {
      "topic": "v1.deliverydron.Delivery001.mission_handler",
      "action": "LOAD_MISSION"
    },
    "data": { }
  }
}
```

The monitor checks `(sender, target.topic, target.action)` and, if allowed, forwards to `target.topic` with `sender` rewritten to `security_monitor` (component id).

---

## 4. Mission handler (ground → drone)

**Topic:** `…mission_handler`

| Action | Purpose |
|--------|---------|
| `LOAD_MISSION` | Accept WPL or JSON mission; validate; forward `mission_load` to autopilot & limiter |
| `VALIDATE_ONLY` | Validate without loading |
| `get_state` | Handler state / last error |

Example `LOAD_MISSION` payload (shape depends on handler implementation; often includes file content or structured steps):

```json
{
  "mission_id": "m-001",
  "format": "wpl",
  "wpl_content": "QGC WPL 110\n..."
}
```

**Policy:** `sender` must be allowed for `(topic=…mission_handler, action=LOAD_MISSION)`. The bundled example policies use `platform` as sender for `LOAD_MISSION` and `VALIDATE_ONLY`.

---

## 5. Autopilot

**Topic:** `…autopilot`

| Action | Purpose |
|--------|---------|
| `mission_load` | Internal: receive mission from mission_handler (via monitor) |
| `cmd` | `START`, `PAUSE`, `RESUME`, `ABORT`, `RESET`, `EMERGENCY_STOP`, `KOVER`, … |
| `get_state` | Mission index, state, cached nav, cargo hint |

`cmd` / `mission_load` are intended to arrive from the security monitor (`sender` prefix `security_monitor`).

---

## 6. Delivery drone service (platform module)

**Binary:** `cmd/delivery_drone`  
**Default topic:** derived like other components → `v1.deliverydron.Delivery001.delivery_drone` when using default env.

Exposes HTTP **health** on `HEALTH_PORT` (default `8080`) at `/health`.

Broker actions (non-exhaustive): `ping`, `get_status`, `echo`, `deliver_package`, `get_delivery_status` — see `src/delivery`.

---

## 7. Telemetry and state

**Topic:** `…telemetry` — action `get_state` returns aggregated motors/cargo snapshot (via internal proxy polling).

**Topic:** `…navigation` — action `get_state` returns last navigation state.

---

## 8. Policy maintenance

If `POLICY_ADMIN_SENDER` is set to your admin client’s `sender` string, that client may call:

- `set_policy`, `remove_policy`, `clear_policies`, `list_policies` on `…security_monitor`.

---

## 9. Upgrading from flat topics

Older deployments used `deliverydron.autopilot-style` names. Set explicitly per service:

```bash
COMPONENT_TOPIC=deliverydron.autopilot
```

Or migrate policies and clients to `v1.deliverydron.Delivery001.autopilot` and remove overrides.

---

## 10. Topic and domain conventions

| Area | Typical choice in this repo |
|------|----------------------------|
| On-board topic segments | `v1.deliverydron.Delivery001.<component>` |
| Payload actuation | `cargo` `OPEN` / `CLOSE` (delivery), not crop sprayer commands |
| Emergency component | `emergency` |
| ATM / port flow on `START` | Not implemented in `src/autopilot` (extension point) |