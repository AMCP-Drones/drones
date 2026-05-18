# Защита внутренних топиков брокера (MQTT ACL)

## Модель

Цель: **изоляция экземпляра БАС** (`INSTANCE_ID`) и **ролевой доступ к топикам**.

| Кто | Публикация (write) | Подписка (read) |
|-----|-------------------|-----------------|
| **security_monitor** | Топик каждого компонента + `replies.<component>` + свой топик | Свой топик + топики компонентов + `replies.*` |
| **Прикладной компонент** | Только `…security_monitor` | Свой топик + `…replies.<component>` |
| **Внешняя система** (опционально) | Только «свой» входной топик (`mission_handler`, `limiter`, `emergency`) | `replies.<component>` при request/response |

Префикс топиков (legacy): `v1.<SYSTEM_NAME>.<INSTANCE_ID>.<component>`  
Пример: `v1.deliverydron.Delivery001.autopilot`.

Ответы request/response: `v1.deliverydron.Delivery001.replies.autopilot` (фиксированный, задаётся в `config.ReplyBrokerTopic()`).

Другой экземпляр (`Delivery002`) использует **другой префикс и другие пароли** — клиент `Delivery001` не может писать в топики `Delivery002`.

Прикладная политика (`security_monitor`, `IsTrustedSender`) остаётся; ACL брокера — **первый рубеж** на транспорте.

## Учётные записи

Имя по умолчанию: `dd_<INSTANCE_ID>_<component_id>` (например `dd_Delivery001_autopilot`).

Переопределение через env (как в `docker-compose.yml`):

- `AUTOPILOT_BROKER_USER` / `AUTOPILOT_BROKER_PASSWORD`
- `SECURITY_MONITOR_BROKER_USER` / `SECURITY_MONITOR_BROKER_PASSWORD`
- … для каждого компонента (`<COMPONENT_ENV_PREFIX>_BROKER_*`)

Компонент при старте вызывает `config.BrokerCredentials()` и передаёт логин/пароль в `bus.New()`.

## Mosquitto (профиль `mqtt`)

1. Скопировать [`docker/mqtt/credentials.env.example`](../docker/mqtt/credentials.env.example) → `docker/mqtt/credentials.env`.
2. Задать уникальные пароли для каждого компонента.
3. При старте контейнера `generate_auth.sh` создаёт `passwd` и `acl` (см. [`docker/mqtt/entrypoint.sh`](../docker/mqtt/entrypoint.sh)).

Проверка ACL локально:

```bash
source docker/.env
go run ./tools/generate_mqtt_auth
```

## Kafka

Клиенты уже поддерживают **SASL PLAIN** (`BROKER_USER` / `BROKER_PASSWORD` на компонент).  
Топикные ACL на брокере Kafka настраиваются отдельно (вне репозитория); используйте те же имена пользователей, что и для MQTT.

## Внешние системы

В `credentials.env`:

```env
MQTT_EXTERNAL_USERS=nus_ext:mission_handler,orvd_ext:limiter,droneport_ext:emergency
NUS_EXT_PASSWORD=...
```

Пользователь `nus_ext` может публиковать только в `…mission_handler`, не в `security_monitor` и не в чужой экземпляр.

## Связанный код

- `config/src/broker_auth.go` — учётные данные и список компонентов
- `bus/auth/mqtt_acl.go` — генерация ACL (тесты)
- `bus/src/mqtt/bus.go` — стабильный `replyTopic` из конфига
- `tools/generate_mqtt_auth` — печать ACL для отладки
