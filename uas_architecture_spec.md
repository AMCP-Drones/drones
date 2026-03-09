# UAS Architecture Specification

**Purpose:** Implementation specification for an Unmanned Aircraft System (UAS / БАС). This document defines components, interfaces, data flows, and constraints so that another agent or team can implement the system.

**Scope:** On-board UAS: external communication, internal communication, safety module, flight controller, navigation, and actuators.

---

## 1. System Boundary and Context

### 1.1 External Entities

| Entity | ID / Alias | Description |
|--------|------------|-------------|
| Data Transmission Environment | DTE | External medium for data to/from the UAS (Среда передачи данных). |
| Communication Module with ATM | ATM_Comms | Interface between DTE and UAS for Air Traffic Management (ОрВД). |

### 1.2 Top-Level Container

- **UAS (БАС):** Contains all on-board systems. All components described below reside inside the UAS boundary unless stated otherwise.

---

## 2. Component Catalog

### 2.1 Communication Layer

#### 2.1.1 Communication Module with UAS (Модуль связи с БАС)

| Attribute | Value |
|----------|--------|
| **Role** | Manages communication with internal UAS components. |
| **Inputs** | Data from Data Transmission Environment (via external link). |
| **Outputs** | Data to: Navigation System; Autopilot (control/telemetry, dashed); Mission (Operator) (control/telemetry, dashed). |
| **Interface note** | Dashed links imply control or telemetry rather than raw sensor stream. |

**Implementation requirements:**

- Accept external data from DTE.
- Route/fan-out to: Navigation System, Autopilot, Mission (Operator).
- Define clear message schemas and topics/channels for each consumer.

---

### 2.2 Navigation

#### 2.2.1 Navigation System (Система навигации)

| Attribute | Value |
|----------|--------|
| **Role** | Provides position and navigation state. |
| **Inputs** | Data from Communication Module with UAS. |
| **Outputs** | Navigation data to: Limiter (Safety Module); Autopilot (Flight Controller). |

**Implementation requirements:**

- Consume data from Communication Module with UAS.
- Produce navigation state (e.g. position, velocity, attitude, time).
- Publish to both Limiter and Autopilot with defined update rate and format.

---

### 2.3 Safety Module (Модуль безопасности)

The Safety Module is a **subsystem**. All of its components share the responsibility of ensuring safe operation and enforcing limits before commands reach actuators.

#### 2.3.1 Mission (Control) — Миссия (контроль)

| Attribute | Value |
|----------|--------|
| **Role** | Mission parameters and control from a safety/oversight perspective. |
| **Inputs** | Control and mission-related data from Communication Module with ATM. |
| **Outputs** | (Implicit: used by other safety components; specify in implementation.) |

#### 2.3.2 Event Log (Журнал событий)

| Attribute | Value |
|----------|--------|
| **Role** | Records significant safety-related events and actions. |
| **Inputs** | Event data from Limiter; event data from Safety Monitor. |
| **Outputs** | Log storage / retrieval API (no direct diagram outputs). |

**Implementation requirements:**

- Append-only or auditable log.
- Subscribers: Limiter, Safety Monitor.
- Define event schema (timestamp, source, severity, payload).

#### 2.3.3 Safety Monitor (Монитор безопасности)

| Attribute | Value |
|----------|--------|
| **Role** | Monitors system parameters and operational status for safety violations. |
| **Inputs** | (Implicit: from various system components; recommend via Broker (IPC).) |
| **Outputs** | Alerts/status to Event Log. |
| **Connections** | Communicates with Broker (IPC). |

#### 2.3.4 Broker (IPC) — Брокер (ІРС)

| Attribute | Value |
|----------|--------|
| **Role** | Inter-Process Communication within the safety module (and optionally with other modules). |
| **Connections** | Safety Monitor; Emergency Systems Control. |

**Implementation requirements:**

- Message bus or broker (e.g. Kafka/MQTT as in project) for IPC.
- Used by Safety Monitor and Emergency Systems Control; define topics and message types.

#### 2.3.5 Limiter (Ограничитель)

| Attribute | Value |
|----------|--------|
| **Role** | Enforces operational constraints; gatekeeper for commands to actuators. All autopilot commands to actuators must pass through the Limiter. |
| **Inputs** | Navigation System; Autopilot (Flight Controller); commands/status from Emergency Systems Control. |
| **Outputs** | Event data to Event Log; **allowed commands to Actuators**. |

**Implementation requirements:**

- **Critical path:** Must run in a safe, deterministic way (consider real-time or safety-critical coding standards).
- Fuse: Navigation + Autopilot commands + Emergency Systems Control state → allow or restrict commands.
- Output only validated/safe commands to Actuators.
- Emit safety events to Event Log when limiting or rejecting.

#### 2.3.6 Emergency Systems Control (Управление аварийными системами)

| Attribute | Value |
|----------|--------|
| **Role** | Activates and controls emergency procedures and systems. |
| **Inputs** | Signals/status from Limiter; data from Broker (IPC). |
| **Outputs** | Commands to **Actuators** (direct). |

**Implementation requirements:**

- Can override or complement normal path in emergencies.
- Consume Broker (IPC) and Limiter outputs; produce actuator commands.
- Define clear handover/override rules with Limiter (e.g. who has priority when).

---

### 2.4 Flight Controller (Полётный контроллер)

The Flight Controller is a **subsystem** responsible for executing flight and mission commands.

#### 2.4.1 Autopilot (Автопилот)

| Attribute | Value |
|----------|--------|
| **Role** | Executes flight plans, maintains stability, produces high-level control. |
| **Inputs** | Navigation data from Navigation System; data/commands (dashed) from Communication Module with UAS. |
| **Outputs** | Control signals to **Limiter**; commands to Release Control; commands to Motor Drives Control. |

**Implementation requirements:**

- Do **not** send commands directly to Actuators; all actuator-bound commands go via Limiter.
- Outputs: (1) to Limiter (for actuator path), (2) to Release Control, (3) to Motor Drives Control.

#### 2.4.2 Mission (Operator) — Миссия (оператор)

| Attribute | Value |
|----------|--------|
| **Role** | Operator mission commands or status (monitoring / command injection). |
| **Inputs** | Data/commands (dashed) from Communication Module with UAS. |
| **Outputs** | (To Autopilot or internal FC logic; specify in implementation.) |

#### 2.4.3 Release Control (Управление сбросом)

| Attribute | Value |
|----------|--------|
| **Role** | Manages payload release or deployment. |
| **Inputs** | Commands from Autopilot. |
| **Outputs** | (To physical release mechanism; may go via Limiter/Actuators depending on design.) |

#### 2.4.4 Motor Drives Control (Управление приводами моторов)

| Attribute | Value |
|----------|--------|
| **Role** | Motor speed and direction. |
| **Inputs** | Commands from Autopilot. |
| **Outputs** | (To physical motors; diagram implies these are ultimately gated by Limiter → Actuators.) |

**Implementation note:** The diagram shows Actuators receiving only from **Limiter** and **Emergency Systems Control**. Therefore Motor Drives Control and Release Control must feed into the path that goes through the Limiter (and then to Actuators), or be modeled as logical sub-actuators that receive from Limiter.

---

### 2.5 Actuators (Приводы)

| Attribute | Value |
|----------|--------|
| **Role** | Physical execution of commands (motors, control surfaces, release mechanisms). |
| **Inputs** | **Only** from Limiter (normal path) and Emergency Systems Control (emergency path). |
| **Outputs** | Physical action. |

**Implementation requirements:**

- Actuators must have exactly two command sources: Limiter and Emergency Systems Control.
- Define priority/arbitration if both can command at once (e.g. emergency overrides normal).

---

## 3. Data and Control Flows (Normative)

Implementations must respect these flows.

| Flow | Path | Description |
|------|------|-------------|
| External command (ATM) | DTE → ATM_Comms → Mission (Control) | Mission/safety oversight from ATM. |
| External control/telemetry | DTE → Comms with UAS → Navigation System, Autopilot, Mission (Operator) | Operator/ground data into UAS. |
| Navigation | Navigation System → Limiter, Autopilot | Nav state to safety and flight control. |
| Autopilot commands | Autopilot → Limiter, Release Control, Motor Drives Control | No direct Autopilot → Actuators. |
| Safety-critical path | Nav + Autopilot + Emergency state → **Limiter** → Event Log, **Actuators** | All normal actuator commands via Limiter. |
| Emergency path | Broker (IPC) ↔ Emergency Systems Control → Limiter, Actuators | Emergency logic can command actuators. |
| Logging | Safety Monitor, Limiter → Event Log | Safety events recorded. |

---

## 4. Constraints and Rules

1. **Single gate to actuators:** All normal commands to Actuators go through the Limiter. Emergency Systems Control may send commands directly to Actuators.
2. **No bypass:** Autopilot must not connect directly to Actuators; only to Limiter, Release Control, and Motor Drives Control.
3. **Safety overlay:** The Safety Module (Limiter + Emergency Systems Control + Safety Monitor + Event Log + Broker) is the safety overlay; it must be able to restrict or override unsafe commands.
4. **Real-time and reliability:** Limiter and Emergency Systems Control are safety-critical; specify timing and reliability (e.g. deadlines, redundancy) in the implementation.
5. **Modularity:** Components must be replaceable or testable in isolation with defined inputs/outputs and interfaces.

---

## 5. Implementation Hints

- **Protocols:** Use project broker (Kafka/MQTT) for IPC (Broker (IPC)); define separate channels/topics per producer-consumer pair where needed.
- **Message format:** Align with existing project convention (e.g. `action`, `payload`, `sender`, `correlation_id`, `reply_to`) for compatibility.
- **Failure modes:** Specify behavior when Navigation, Autopilot, or Communication Module fail (e.g. Limiter defaults to safe hold, Emergency Systems Control takes over).
- **Testing:** Provide simulators or mocks for DTE, Navigation System, and Autopilot so that Safety Module and Flight Controller can be tested without hardware.

---

## 6. Glossary (Russian / English)

| Russian | English |
|---------|---------|
| БАС | UAS (Unmanned Aircraft System) |
| ОрВД | ATM (Air Traffic Management) |
| Модуль связи с БАС | Communication Module with UAS |
| Система навигации | Navigation System |
| Модуль безопасности | Safety Module |
| Миссия (контроль) | Mission (Control) |
| Журнал событий | Event Log |
| Монитор безопасности | Safety Monitor |
| Брокер (ІРС) | Broker (IPC) |
| Ограничитель | Limiter |
| Управление аварийными системами | Emergency Systems Control |
| Полётный контроллер | Flight Controller |
| Автопилот | Autopilot |
| Миссия (оператор) | Mission (Operator) |
| Управление сбросом | Release Control |
| Управление приводами моторов | Motor Drives Control |
| Приводы | Actuators |
| Среда передачи данных | Data Transmission Environment |

---

**Document version:** 1.0  
**Reference:** UAS architecture diagram (image-e0c1c123-b06b-4af1-9a98-a807602a9458.png)
