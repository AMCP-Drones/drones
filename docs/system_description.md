# Описание системы и кибериммунной архитектуры (актуализация)

Дата актуализации: 11 мая 2026

---

## 0) Контекстная диаграмма и функциональная архитектура

### 0.1 Контекстная диаграмма

```mermaid
flowchart LR
  nus[НУС]
  orvd[ОРВДМодульБезопасности]
  dronePort[Дронопорт]
  sitl[SITL]
  bas[DeliveryBAS]
  analytics[DroneAnalytics]

  nus -->|"deliverydron.mission_handler"| bas
  orvd -->|"deliverydron.limiter"| bas
  dronePort -->|"deliverydron.emergency"| bas
  bas -->|"analytics.telemetry"| analytics
  bas -->|"analytics.events"| analytics
  bas -->|"sitl.commands"| sitl
  sitl -->|"deliverydron.security_monitor"| bas
```

### 0.2 Функциональная архитектура системы

```mermaid
flowchart TD
  missionHandler[mission_handler]
  autopilot[autopilot]
  limiter[limiter]
  emergency[emergency]
  telemetry[telemetry]
  sitl[SITL]
  analytics[DroneAnalytics]
  navigation[navigation]
  motors[motors]
  cargo[cargo]
  journal[journal]
  securityMonitor[security_monitor]

  missionHandler -->|"deliverydron.security_monitor"| securityMonitor
  autopilot -->|"deliverydron.security_monitor"| securityMonitor
  limiter -->|"deliverydron.security_monitor"| securityMonitor
  emergency -->|"deliverydron.security_monitor"| securityMonitor
  telemetry -->|"deliverydron.security_monitor"| securityMonitor

  securityMonitor -->|"deliverydron.navigation"| navigation
  securityMonitor -->|"deliverydron.motors"| motors
  securityMonitor -->|"deliverydron.cargo"| cargo
  securityMonitor -->|"deliverydron.journal"| journal
  motors -->|"sitl.commands"| sitl
  sitl -->|"deliverydron.security_monitor"| securityMonitor
  journal -->|"analytics.telemetry"| analytics
  journal -->|"analytics.events"| analytics
```

### 0.3 Диаграмма архитектуры политики

![](system_description.drawio.png)

---

## 1) ЦБПБ

### 1.1 Цели безопасности (ЦБ)
1. К критичным компонентам БАС допускаются только аутентичные и авторизованные сообщения.
1. Все межкомпонентные критичные операции являются авторизованными.
1. Обрабатываются только авторизованные, корректно адресованные и разрешённые сообщения.
1. Критичные исполнительные компоненты `navigation`, `motors`, `cargo` выполняют только авторизованные операции.

### 1.2 Предположения безопасности (ПБ)
1. Бизнес-валидность payload команд обеспечивается внешними доверенными контурами.
2. Аварийная логика детекции/реакции верифицируется вне message-control/исполнительного trust-контура.
3. Защита транспортного канала (К/Ц/П) обеспечивается инфраструктурой защищённой связи.

---

## 2) Архитектура политики, уровни доверия и оценка доменов

### 2.1 Архитектура политики

- `security_monitor` реализует policy enforcement для `proxy_request`/`proxy_publish`.
- Политики задаются списком `(sender, topic, action)`.
- При переходе в `ISOLATED` активируется аварийный набор политик, при `ISOLATION_END` восстанавливаются базовые политики.
- Deny-by-default: отсутствие правила = запрет.

**Ссылка на политики безопасности:**
- [`security_monitor/security_monitor.env`](../security_monitor/security_monitor.env)
- дублирующая сборочная копия: [`src/security_monitor/security_monitor.env`](../src/security_monitor/security_monitor.env)

### 2.2 Обоснование уровней доверия (по угрозам и путям атак)

- **L0 (недоверенный ввод):** любые внешние/межсервисные сообщения до проверки.
  - Угроза: подмена sender, навязывание команды.
  - Контроль: централизованный policy check в `security_monitor`.
- **L1 (условно доверенный отправитель):** sender, разрешённый политикой для конкретного `(topic, action)`.
  - Угроза: злоупотребление разрешённым каналом.
  - Контроль: точное сопоставление sender + action/topic + режим isolation.
- **L2 (доверенный механизм управления состоянием безопасности):** `security_monitor` как держатель `NORMAL/ISOLATED`.
  - Угроза: обход режима isolation.
  - Контроль: аварийные политики + переходы `ISOLATION_START/END`, watchdog-failsafe.
- **L3 (доверенный аудит):** `journal` как самописец решений allow/deny и переходов.
  - Угроза: потеря/подмена следов.
  - Контроль: централизованная запись security-событий, проверка в тестах.
- **L4 (доверенные исполнительные модули):** `navigation`, `motors`, `cargo` как часть ДВБ для критичных операций.
  - Угроза: исполнение команды в обход policy-gate.
  - Контроль: mediation-only доступ через `security_monitor` + тесты разрешенных/запрещенных сценариев.

### 2.3 Оценка доменов безопасности по размеру и сложности

Метрика размера: непустые строки Go-кода (`*.go`), без `vendor`, `tests`, `.generated`, `*_test.go`.

#### ДВБ для текущих ЦБПБ (message-control + trusted executors TCB)

- `security_monitor` — 617 LOC (высокая сложность)
- `component` — 278 LOC (средняя сложность)
- `config` — 155 LOC (низкая/средняя сложность)
- `journal` — 188 LOC (средняя сложность)
- `navigation` — 139 LOC (низкая/средняя сложность)
- `motors` — 206 LOC (средняя сложность)
- `cargo` — 151 LOC (низкая/средняя сложность)

**Итого ДВБ: 1734 LOC**

#### Вне ДВБ в текущей постановке ЦБПБ

`autopilot`, `mission_handler`, `limiter`, `emergency`, `telemetry`, `bus`, `sdk`, `delivery*`, `stub_component` — вынесены в предположения безопасности/внешние контуры ответственности для данного scope.

---

## 3) Внедрённые шаблоны СКИБ

1. **Reference Monitor / Policy Enforcement Point**  
   `security_monitor` — единая точка mediating доступа.
2. **Mediation-only access**  
   Критичные операции идут через `proxy_request`/`proxy_publish`.
3. **Fail-safe default**  
   Нет разрешающей политики -> отказ (`forbidden`).
4. **Dedicated Safety-State Mechanism**  
   Централизованное состояние безопасности (`NORMAL/ISOLATED`) и управляемые переходы `ISOLATION_START/ISOLATION_END`.
5. **Defense in Depth (внутри message-control + executors контура)**  
   Policy check + exact sender match + isolation mode + trusted executors + audit trail.

---

## Проверка соответствия политик архитектуре

Совместно с тестовым контуром выполнена проверка policy-сценариев:

- `go test ./tests -run 'SecurityMonitor|Safety|Integration_MissionHandler_ProxyPolicy|Integration_Cargo_Journal' -count=1`
- Результат: **PASS** (`ok .../tests`).

Покрыто:
- deny-by-default;
- разрешённая проксируемая публикация/запрос;
- переход в `ISOLATED` и аварийные разрешения;
- восстановление в `NORMAL` (`ISOLATION_END`);
- контроль trusted sender и журналирование security-событий.
