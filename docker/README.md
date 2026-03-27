# Running the delivery drone with the platform broker

The **parent repository** (broker, SDK, scripts, secrets) is:

**`/home/michael/Development/SPBU/sbd-drones-economics`**  
See also: **`docs/quick_start.md`** in the parent repo.

## 1. Start the broker (parent repo)

Follow the parent’s quick start: create `docker/.env` from the example, then bring up the broker:

```bash
cd /home/michael/Development/SPBU/sbd-drones-economics
cp docker/example.env docker/.env
make docker-up
```

This starts Kafka (and optionally MQTT) and creates the `drones_net` network. Kafka uses `ADMIN_USER` / `ADMIN_PASSWORD` from `docker/.env` for SASL.

## 2. Run the delivery drone (this repo)

**Option A — Makefile (single container):**

```bash
cd /home/michael/Development/SPBU/drones
make docker-up
```

Defaults use `BROKER_USER=admin` and `BROKER_PASSWORD=admin_secret_123` (same as parent `docker/example.env`). If you followed the parent quick start with `cp docker/example.env docker/.env`, `make docker-up` here works as-is. If your parent `docker/.env` uses different credentials:

```bash
BROKER_USER=admin BROKER_PASSWORD=<your_admin_password> make docker-up
```

**Option B — Docker Compose:**

From this repo root, with an env file that sets `BROKER_USER` and `BROKER_PASSWORD` (e.g. from parent’s `docker/.env` or same values):

```bash
docker compose -f docker/docker-compose.yml --env-file docker/example.env up -d
```

Ensure `docker/example.env` or your env file has `KAFKA_BOOTSTRAP_SERVERS=kafka:29092` and `BROKER_USER` / `BROKER_PASSWORD` when the stack runs in Docker (so the service reaches Kafka on `drones_net`).

## 3. Check

- Health: `curl -s http://localhost:8080/health` → `ok`
- Logs: `docker logs -f delivery_drone`

With default env (`TOPIC_VERSION`, `SYSTEM_NAME`, `INSTANCE_ID` from `docker/example.env`), the delivery drone listens on `v1.deliverydron.Delivery001.delivery_drone`. Override with `COMPONENT_TOPIC` (legacy examples used `components.delivery_drone`). Actions include `ping`, `get_status`, `echo`, `deliver_package`, `get_delivery_status`. See repo `docs/SYSTEM.md`.
