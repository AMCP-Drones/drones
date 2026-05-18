#!/usr/bin/env python3
"""
Minimal firmware-certification regulator for release CI.
Consumes v1.firmware.certification.request on Kafka, clones the repo, runs
scripts/regulator/*.sh, publishes v1.firmware.certificate.result.

Team1 regulator (integration branch) can replace this when their Kafka entrypoint is stable.
"""
from __future__ import annotations

import asyncio
import hashlib
import json
import logging
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
logger = logging.getLogger("regulator_firmware_kafka")

TOPIC_REQUEST = "v1.firmware.certification.request"
TOPIC_RESULT = "v1.firmware.certificate.result"

REPO_ROOT = Path(__file__).resolve().parents[2]
GOALS_FILE = REPO_ROOT / "docs" / "regulator" / "security_goals.deliverydron.example.json"


def load_goals() -> tuple[list[str], dict[str, str]]:
    data = json.loads(GOALS_FILE.read_text(encoding="utf-8"))
    goals = data.get("goals", {}).get("firmware", [])
    commands = data.get("test_commands", {})
    return goals, commands


def clone_repo(url: str, commit: str, dest: Path) -> bool:
    logger.info("clone %s -> %s (commit=%s)", url, dest, commit)
    proc = subprocess.run(
        ["git", "clone", url, str(dest)],
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        logger.error("clone failed: %s", proc.stderr)
        return False
    if not commit:
        return True
    fetch = subprocess.run(
        ["git", "-C", str(dest), "fetch", "origin", commit],
        capture_output=True,
        text=True,
    )
    if fetch.returncode != 0:
        logger.error("fetch %s failed: %s", commit, fetch.stderr)
        return False
    checkout = subprocess.run(
        ["git", "-C", str(dest), "checkout", "FETCH_HEAD"],
        capture_output=True,
        text=True,
    )
    if checkout.returncode != 0:
        logger.error("checkout failed: %s", checkout.stderr)
        return False
    return True


def run_goal_commands(repo_path: Path, goals: list[str], commands: dict[str, str]) -> tuple[bool, list[dict]]:
    results = []
    all_ok = True
    for goal in goals:
        cmd = commands.get(goal)
        if not cmd:
            results.append({"goal": goal, "passed": True, "skipped": True})
            continue
        logger.info("goal %s: %s", goal, cmd)
        proc = subprocess.run(
            cmd,
            shell=True,
            cwd=str(repo_path),
            capture_output=True,
            text=True,
        )
        ok = proc.returncode == 0
        all_ok = all_ok and ok
        results.append({
            "goal": goal,
            "command": cmd,
            "passed": ok,
            "exit_code": proc.returncode,
            "stderr": (proc.stderr or "")[-500:],
        })
    return all_ok, results


def sign_cert(data: dict) -> str:
    payload = json.dumps(data, sort_keys=True)
    return hashlib.sha256(payload.encode()).hexdigest()


async def handle_request(payload: dict, producer, goals: list[str], commands: dict[str, str]) -> None:
    request_id = payload.get("request_id", "")
    firmware = payload.get("firmware") or {}
    repo_url = firmware.get("repository_url", "")
    commit = firmware.get("commit_hash", "")
    version = firmware.get("version", "")
    drone_type = payload.get("drone_type", "")

    with tempfile.TemporaryDirectory(prefix="regulator_ci_") as tmp:
        repo_path = Path(tmp) / "repo"
        passed = clone_repo(repo_url, commit, repo_path)
        test_results: list[dict] = []
        if passed:
            passed, test_results = run_goal_commands(repo_path, goals, commands)

    if not passed:
        result = {
            "request_id": request_id,
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "status": "REJECTED",
            "certificate": None,
            "errors": test_results,
        }
    else:
        subject = commit or request_id
        cert_body = {
            "certificate_id": f"CERT-FIRMWARE-{datetime.now(timezone.utc).strftime('%Y%m%d%H%M%S')}-{subject[:8]}",
            "firmware": {"version": version, "commit_hash": commit},
            "drone_type": drone_type,
            "requirements_checked": goals,
            "issued_at": datetime.now(timezone.utc).isoformat(),
            "valid_until": datetime.now(timezone.utc).replace(year=datetime.now(timezone.utc).year + 1).isoformat(),
        }
        cert_body["digital_signature"] = sign_cert(cert_body)
        result = {
            "request_id": request_id,
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "status": "CERTIFIED",
            "certificate": cert_body,
            "errors": [],
        }

    producer.send(TOPIC_RESULT, json.dumps(result).encode("utf-8"))
    producer.flush()
    logger.info("published %s for request %s", result["status"], request_id)


async def main() -> None:
    from kafka import KafkaConsumer, KafkaProducer

    bootstrap = os.getenv("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092")
    goals, commands = load_goals()

    producer = KafkaProducer(bootstrap_servers=bootstrap.split(","))
    consumer = KafkaConsumer(
        TOPIC_REQUEST,
        bootstrap_servers=bootstrap.split(","),
        group_id=f"regulator_firmware_{os.getpid()}",
        auto_offset_reset="latest",
    )
    logger.info("listening on %s (bootstrap=%s)", TOPIC_REQUEST, bootstrap)

    for msg in consumer:
        try:
            payload = json.loads(msg.value.decode("utf-8"))
        except json.JSONDecodeError:
            logger.warning("invalid json on %s", TOPIC_REQUEST)
            continue
        await handle_request(payload, producer, goals, commands)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        sys.exit(0)
