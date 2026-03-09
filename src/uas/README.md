# UAS Architecture (On-board)

This package is a placeholder for the **UAS Architecture** per `uas_architecture_spec.md`.

Components to implement (aligned with spec):

- **Communication**: DTE → Communication Module with UAS → Navigation, Autopilot, Mission (Operator).
- **Navigation**: Consume from Comms, produce nav state to Limiter and Autopilot.
- **Safety Module**: Mission (Control), Event Log, Safety Monitor, Broker (IPC), Limiter, Emergency Systems Control.
- **Flight Controller**: Autopilot, Mission (Operator), Release Control, Motor Drives Control.
- **Actuators**: Inputs only from Limiter (normal) and Emergency Systems Control (emergency).

Rules:

- All normal actuator commands go through the **Limiter**; no Autopilot → Actuators direct.
- Broker (Kafka/MQTT) for IPC; message format: `action`, `payload`, `sender`, `correlation_id`, `reply_to`, `timestamp`.

The delivery drone service uses the same broker and message protocol; UAS components can be added as internal packages and wired via the same bus.
