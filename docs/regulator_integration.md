# Regulator firmware certification

Deliverydron obtains a **firmware certificate** from the course regulator over Kafka at **release** time. The certificate ID is written to [`docker/.env`](../docker/.env) as `ORVD_CERTIFICATE_ID` and passed to the `limiter` for OpBD `register_drone`.

## Topics

| Direction | Topic |
|-----------|--------|
| Request | `v1.firmware.certification.request` |
| Response | `v1.firmware.certificate.result` |

## Request payload

```json
{
  "request_id": "req-…",
  "timestamp": "2026-05-18T12:00:00Z",
  "developer_id": "AMCP-Drones",
  "drone_type": "DeliveryDrone-X2",
  "firmware": {
    "repository_url": "https://github.com/AMCP-Drones/drones",
    "commit_hash": "<release-sha>",
    "version": "<release-tag>"
  }
}
```

On success the regulator responds with `status: "CERTIFIED"` and `certificate.certificate_id`.

## Security goals (regulator CI)

The regulator clones the repository and runs shell commands from [`docs/regulator/security_goals.deliverydron.example.json`](regulator/security_goals.deliverydron.example.json). Implementations live under [`scripts/regulator/`](../scripts/regulator/).

Team1 should merge the example into their `security_goals.json` when using the full Python regulator. Release CI uses [`scripts/ci/regulator_firmware_kafka.py`](../scripts/ci/regulator_firmware_kafka.py) with the same goal mapping.

| Goal ID | Script | Maps to |
|---------|--------|---------|
| FW-SEC-01 | `fw_sec_01.sh` | CB-1 access control |
| FW-SEC-02 | `fw_sec_02.sh` | CB-2 PEP |
| FW-SEC-03 | `fw_sec_03.sh` | CB-3 fail-safe deny |
| FW-SEC-05 | `fw_sec_05.sh` | CB-5 execution gate |
| SYS-SEC-01 | `sys_sec_01.sh` | CB-4 audit journal |

Dry-run locally:

```bash
bash scripts/regulator/run_all.sh
bash check_coverage.sh
```

## Release CI

Workflow [`.github/workflows/firmware-cert.yml`](../.github/workflows/firmware-cert.yml):

1. Starts Kafka and the firmware regulator shim.
2. Runs `go run ./cmd/regulator_cert` for the release commit.
3. Updates `docker/.env` with `ORVD_CERTIFICATE_ID`.
4. Commits `docker/.env` and `data/firmware_certificate.json` back to the release tag.

Manual run:

```bash
gh workflow run firmware-cert.yml -f commit=$(git rev-parse HEAD) -f version=v0.0.0-test
```

## CLI

```bash
go run ./cmd/regulator_cert \
  --kafka localhost:9092 \
  --repo https://github.com/AMCP-Drones/drones \
  --commit "$(git rev-parse HEAD)" \
  --version "$(git describe --tags --always)" \
  --env-file docker/.env
```

Environment variables: `KAFKA_BOOTSTRAP_SERVERS`, `REGULATOR_REPO_URL`, `GITHUB_SHA`, `GITHUB_REF_NAME`, `REGULATOR_DEVELOPER_ID`, `REGULATOR_DRONE_TYPE`.

## Local end-to-end

1. Start Kafka (plaintext listener on `9092`).
2. `pip install kafka-python && python scripts/ci/regulator_firmware_kafka.py`
3. Run `regulator_cert` as above.
4. Confirm `grep ORVD_CERTIFICATE_ID docker/.env`.

## Related

- [ORVD integration](orvd_integration.md) — `ORVD_CERTIFICATE_ID` on `register_drone`
- [Broker topic auth](broker_topic_auth.md)
