#!/usr/bin/env python3
"""
Build system: merge broker infrastructure (docker/docker-compose.yml) with
system components into a single docker-compose.yml and .env.

Usage:
    python scripts/prepare_system.py <system_dir>

Example:
    python scripts/prepare_system.py systems/deliverydron
"""
import sys
import os
from pathlib import Path
from copy import deepcopy

import yaml


def to_env_prefix(name: str) -> str:
    """Convert service/component name to ENV-safe prefix."""
    return "".join(ch if ch.isalnum() else "_" for ch in name).upper()


def parse_env_file(path: Path) -> dict:
    env = {}
    if not path.exists():
        return env
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" in line:
            key, _, value = line.partition("=")
            env[key.strip()] = value.strip()
    return env


def write_env_file(path: Path, env: dict):
    """Write env dict; quote values that contain quotes, backslashes, or newlines."""
    with open(path, "w") as f:
        for key, value in env.items():
            s = str(value)
            if '"' in s or "\n" in s or "\\" in s:
                escaped = s.replace("\\", "\\\\").replace('"', '\\"')
                f.write(f'{key}="{escaped}"\n')
            else:
                f.write(f"{key}={s}\n")


def rewrite_path(original: str, from_dir: Path, to_dir: Path) -> str:
    """Rewrite a relative path: resolve it from from_dir, then make relative to to_dir."""
    abs_path = (from_dir / original).resolve()
    return os.path.relpath(abs_path, to_dir.resolve())


def rewrite_volumes(volumes: list, from_dir: Path, to_dir: Path) -> list:
    result = []
    for vol in volumes:
        if isinstance(vol, dict):
            vol_copy = vol.copy()
            if "source" in vol_copy:
                source = vol_copy["source"]
                if not source.startswith("/") and not source.startswith("$"):
                    vol_copy["source"] = rewrite_path(source, from_dir, to_dir)
            result.append(vol_copy)
        else:
            parts = vol.split(":")
            if len(parts) >= 2 and not parts[0].startswith("/") and not parts[0].startswith("$"):
                parts[0] = rewrite_path(parts[0], from_dir, to_dir)
            result.append(":".join(parts))
    return result


def prepare_system(system_dir: str):
    root = Path(__file__).resolve().parent.parent
    system_path = root / system_dir

    if not system_path.is_dir():
        print(f"Error: system directory '{system_path}' not found", file=sys.stderr)
        sys.exit(1)

    broker_compose_path = root / "docker" / "docker-compose.yml"
    system_compose_path = system_path / "docker-compose.yml"

    for path, label in [
        (broker_compose_path, "broker compose"),
        (system_compose_path, "system compose"),
    ]:
        if not path.exists():
            print(f"Error: {label} '{path}' not found", file=sys.stderr)
            sys.exit(1)

    broker_compose = yaml.safe_load(broker_compose_path.read_text())
    system_compose = yaml.safe_load(system_compose_path.read_text())

    docker_dir = root / "docker"
    root_env = parse_env_file(docker_dir / ".env")
    if not root_env and (docker_dir / "example.env").exists():
        root_env = parse_env_file(docker_dir / "example.env")

    # Committed defaults (paths are not named .env so they stay in git; see .gitignore)
    deliverydron_defaults = parse_env_file(system_path / "deliverydron.env")
    system_local_overrides = parse_env_file(system_path / ".env")

    # Discover components: prefer components/<name>/.env, else components/<name>/<name>.env
    components_dir = root / "components"
    component_envs = {}
    if components_dir.is_dir():
        for comp_dir in sorted(components_dir.iterdir()):
            if not comp_dir.is_dir():
                continue
            env_file = comp_dir / ".env"
            if not env_file.exists():
                env_file = comp_dir / f"{comp_dir.name}.env"
            if env_file.exists():
                component_envs[comp_dir.name] = parse_env_file(env_file)

    output_dir = system_path / ".generated"
    output_dir.mkdir(exist_ok=True)

    # --- Build merged .env ---
    merged_env = dict(root_env)
    merged_env.update(deliverydron_defaults)
    merged_env.update(system_local_overrides)
    suffixes = []
    for i, (comp_name, env) in enumerate(component_envs.items()):
        prefix = to_env_prefix(comp_name)
        for key, value in env.items():
            merged_env[f"{prefix}_{key}"] = value

        suffix = chr(ord("A") + i)
        suffixes.append(suffix)
        merged_env[f"COMPONENT_USER_{suffix}"] = env.get("BROKER_USER", "")
        merged_env[f"COMPONENT_PASSWORD_{suffix}"] = env.get("BROKER_PASSWORD", "")

    # Topic identity defaults for hierarchical component topics
    if "SYSTEM_NAME" not in merged_env:
        merged_env["SYSTEM_NAME"] = "deliverydron"
    if "TOPIC_VERSION" not in merged_env:
        merged_env["TOPIC_VERSION"] = "v1"
    if "INSTANCE_ID" not in merged_env:
        merged_env["INSTANCE_ID"] = "Delivery001"

    sys_name = merged_env.get("SYSTEM_NAME", "deliverydron")
    topic_ver = merged_env.get("TOPIC_VERSION", "v1")
    instance_id = merged_env.get("INSTANCE_ID", "Delivery001")
    topic_prefix = f"{topic_ver}.{sys_name}.{instance_id}"
    ext_substitutions = {
        "${TOPIC_PREFIX}": topic_prefix,
        "${SYSTEM_NAME}": sys_name,
        "$${SYSTEM_NAME}": sys_name,
        "$SYSTEM_NAME": sys_name,
        "${NUS_TOPIC}": merged_env.get("NUS_TOPIC", ""),
        "${ORVD_TOPIC}": merged_env.get("ORVD_TOPIC", ""),
        "${DRONEPORT_TOPIC}": merged_env.get("DRONEPORT_TOPIC", ""),
        "${SITL_TOPIC}": merged_env.get("SITL_TOPIC", ""),
        "${SITL_COMMANDS_TOPIC}": merged_env.get("SITL_COMMANDS_TOPIC", ""),
        "${SITL_TELEMETRY_REQUEST_TOPIC}": merged_env.get(
            "SITL_TELEMETRY_REQUEST_TOPIC", ""
        ),
    }
    for key in list(merged_env.keys()):
        if "SECURITY_POLICIES" in key and isinstance(merged_env.get(key), str):
            val = merged_env[key]
            for placeholder, replacement in ext_substitutions.items():
                val = val.replace(placeholder, replacement)
            merged_env[key] = val

    # --- Rewrite broker volume paths ---
    broker_dir = broker_compose_path.parent
    broker_services = deepcopy(broker_compose.get("services", {}))
    for svc_name, svc in broker_services.items():
        if "volumes" in svc:
            svc["volumes"] = rewrite_volumes(svc["volumes"], broker_dir, output_dir)

        # Update broker env: replace hardcoded COMPONENT_USER_* with discovered ones
        env_block = svc.get("environment", {})
        if isinstance(env_block, list):
            new_env = {}
            for item in env_block:
                k, _, v = item.partition("=")
                new_env[k.strip()] = v.strip()
            env_block = new_env

        keys_to_remove = [
            k
            for k in env_block
            if k.startswith("COMPONENT_USER_") or k.startswith("COMPONENT_PASSWORD_")
        ]
        for k in keys_to_remove:
            del env_block[k]

        for suffix in suffixes:
            env_block[f"COMPONENT_USER_{suffix}"] = f"${{COMPONENT_USER_{suffix}:-}}"
            env_block[f"COMPONENT_PASSWORD_{suffix}"] = f"${{COMPONENT_PASSWORD_{suffix}:-}}"

        svc["environment"] = env_block

    # --- Rewrite component build paths ---
    system_dir_abs = system_compose_path.parent
    system_dir_prefix = system_dir + "/"  # e.g. systems/deliverydron/
    component_services = deepcopy(system_compose.get("services", {}))
    for svc_name, svc in component_services.items():
        if "build" in svc:
            build = svc["build"]
            if isinstance(build, dict):
                if "context" in build:
                    build["context"] = rewrite_path(build["context"], system_dir_abs, output_dir)
                # Dockerfile path: from .generated/ either ../ (legacy system src) or ../../components/...
                if "dockerfile" in build:
                    df = build["dockerfile"]
                    if df.startswith(system_dir_prefix):
                        build["dockerfile"] = "../" + df[len(system_dir_prefix):]
                    elif df.startswith("components/"):
                        build["dockerfile"] = rewrite_path(df, root, output_dir)

        # Add depends_on for broker health checks
        existing_depends = svc.get("depends_on", {})
        if not isinstance(existing_depends, dict):
            existing_depends = {}
        existing_depends.update({
            "kafka": {"condition": "service_healthy", "required": False},
            "mosquitto": {"condition": "service_healthy", "required": False},
        })
        svc["depends_on"] = existing_depends

    # --- Merge into single compose ---
    merged = {
        "name": "drones",
        "services": {},
        "networks": {
            "drones_net": {
                "driver": "bridge",
                "name": "${DOCKER_NETWORK:-drones_net}",
            }
        },
    }

    for svc_name, svc in broker_services.items():
        merged["services"][svc_name] = svc

    for svc_name, svc in component_services.items():
        merged["services"][svc_name] = svc

    # --- Merge top-level volumes (for persistent component storage) ---
    broker_volumes = deepcopy(broker_compose.get("volumes", {})) or {}
    system_volumes = deepcopy(system_compose.get("volumes", {})) or {}
    if broker_volumes or system_volumes:
        merged["volumes"] = {}
        merged["volumes"].update(broker_volumes)
        merged["volumes"].update(system_volumes)

    # --- Write output ---
    compose_out = output_dir / "docker-compose.yml"
    env_out = output_dir / ".env"

    with open(compose_out, "w") as f:
        f.write(
            "# AUTO-GENERATED by scripts/prepare_system.py\n"
            "# Do not edit manually. Re-run: python scripts/prepare_system.py "
            f"{system_dir}\n"
        )
        yaml.dump(merged, f, default_flow_style=False, sort_keys=False, allow_unicode=True)

    write_env_file(env_out, merged_env)

    print(f"Generated: {compose_out}")
    print(f"Generated: {env_out}")
    print(f"Components: {', '.join(component_envs.keys())}")
    print(f"Credentials mapped: {', '.join(f'COMPONENT_USER_{s}' for s in suffixes)}")
    print()
    print("To start:")
    broker_type = merged_env.get("BROKER_TYPE", "kafka")
    print(
        f"  docker compose -f {compose_out} --env-file {env_out} "
        f"--profile {broker_type} up -d --build"
    )


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print(
            "Usage: python scripts/prepare_system.py <system_dir>",
            file=sys.stderr,
        )
        sys.exit(1)
    prepare_system(sys.argv[1])