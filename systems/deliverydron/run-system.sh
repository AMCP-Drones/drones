#!/usr/bin/env bash
set -euo pipefail

SYSTEM_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GENERATED_DIR="${SYSTEM_DIR}/.generated"
BROKER_TYPE="${BROKER_TYPE:-kafka}"
ACTION="${1:-up}"

COMPONENTS=(
  autopilot
  cargo
  delivery_drone
  emergency
  journal
  limiter
  mission_handler
  motors
  navigation
  security_monitor
  telemetry
)

log() {
  printf '[deliverydron] %s\n' "$*"
}

die() {
  printf '[deliverydron] ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

detect_paths() {
  local cursor="$SYSTEM_DIR"
  GO_ROOT=""
  PREPARE_SCRIPT=""
  BROKER_COMPOSE=""

  while :; do
    if [[ -z "${GO_ROOT}" && -f "${cursor}/go.mod" && -d "${cursor}/cmd" ]]; then
      GO_ROOT="$cursor"
    fi
    if [[ -z "${PREPARE_SCRIPT}" && -f "${cursor}/scripts/prepare_system.py" ]]; then
      PREPARE_SCRIPT="${cursor}/scripts/prepare_system.py"
    fi
    if [[ -z "${BROKER_COMPOSE}" && -f "${cursor}/docker/docker-compose.yml" ]]; then
      BROKER_COMPOSE="${cursor}/docker/docker-compose.yml"
    fi

    if [[ -n "${GO_ROOT}" && -n "${PREPARE_SCRIPT}" && -n "${BROKER_COMPOSE}" ]]; then
      break
    fi

    local parent
    parent="$(dirname "${cursor}")"
    if [[ "${parent}" == "${cursor}" ]]; then
      break
    fi
    cursor="${parent}"
  done
}

ensure_python_yaml() {
  if python3 - <<'PY' >/dev/null 2>&1
import yaml
PY
  then
    return
  fi

  log "PyYAML not found. Installing locally for current user..."
  python3 -m pip install --user pyyaml >/dev/null
}

prepare_go() {
  [[ -n "${GO_ROOT}" ]] || die "Could not locate go.mod + cmd/ in this repo tree."
  require_cmd go

  log "Using Go root: ${GO_ROOT}"
  pushd "${GO_ROOT}" >/dev/null

  log "Downloading Go modules..."
  go mod download

  log "Vendoring Go modules for Docker builds..."
  go mod vendor

  mkdir -p "${GENERATED_DIR}/bin"
  for component in "${COMPONENTS[@]}"; do
    log "Building cmd/${component}..."
    go build -mod=vendor -o "${GENERATED_DIR}/bin/${component}" "./cmd/${component}"
  done

  popd >/dev/null
}

compose_up() {
  require_cmd docker

  if [[ -n "${PREPARE_SCRIPT}" ]]; then
    ensure_python_yaml
    log "Preparing merged compose via ${PREPARE_SCRIPT}..."
    python3 "${PREPARE_SCRIPT}" "systems/deliverydron"

    local generated_compose="${GENERATED_DIR}/docker-compose.yml"
    local generated_env="${GENERATED_DIR}/.env"
    [[ -f "${generated_compose}" ]] || die "Expected generated compose file at ${generated_compose}"
    [[ -f "${generated_env}" ]] || die "Expected generated env file at ${generated_env}"

    log "Starting stack with profile: ${BROKER_TYPE}"
    docker compose -f "${generated_compose}" --env-file "${generated_env}" --profile "${BROKER_TYPE}" up -d --build
    return
  fi

  [[ -n "${BROKER_COMPOSE}" ]] || die "Could not locate docker/docker-compose.yml for broker services."
  log "prepare_system.py not found; using compose fallback merge."
  log "Starting stack with profile: ${BROKER_TYPE}"
  docker compose -f "${BROKER_COMPOSE}" -f "${SYSTEM_DIR}/docker-compose.yml" --env-file "${SYSTEM_DIR}/deliverydron.env" --profile "${BROKER_TYPE}" up -d --build
}

compose_down() {
  require_cmd docker
  if [[ -f "${GENERATED_DIR}/docker-compose.yml" && -f "${GENERATED_DIR}/.env" ]]; then
    docker compose -f "${GENERATED_DIR}/docker-compose.yml" --env-file "${GENERATED_DIR}/.env" --profile kafka down || true
    docker compose -f "${GENERATED_DIR}/docker-compose.yml" --env-file "${GENERATED_DIR}/.env" --profile mqtt down || true
    return
  fi

  [[ -n "${BROKER_COMPOSE}" ]] || die "Could not locate compose configuration for shutdown."
  docker compose -f "${BROKER_COMPOSE}" -f "${SYSTEM_DIR}/docker-compose.yml" --env-file "${SYSTEM_DIR}/deliverydron.env" --profile kafka down || true
  docker compose -f "${BROKER_COMPOSE}" -f "${SYSTEM_DIR}/docker-compose.yml" --env-file "${SYSTEM_DIR}/deliverydron.env" --profile mqtt down || true
}

compose_logs() {
  require_cmd docker
  if [[ -f "${GENERATED_DIR}/docker-compose.yml" && -f "${GENERATED_DIR}/.env" ]]; then
    docker compose -f "${GENERATED_DIR}/docker-compose.yml" --env-file "${GENERATED_DIR}/.env" --profile "${BROKER_TYPE}" logs -f
    return
  fi

  [[ -n "${BROKER_COMPOSE}" ]] || die "Could not locate compose configuration for logs."
  docker compose -f "${BROKER_COMPOSE}" -f "${SYSTEM_DIR}/docker-compose.yml" --env-file "${SYSTEM_DIR}/deliverydron.env" --profile "${BROKER_TYPE}" logs -f
}

usage() {
  cat <<'EOF'
Usage:
  ./run-system.sh [up|down|logs|build]

Environment:
  BROKER_TYPE=kafka|mqtt   (default: kafka)
EOF
}

main() {
  detect_paths

  case "${ACTION}" in
    up)
      prepare_go
      compose_up
      ;;
    build)
      prepare_go
      ;;
    down)
      compose_down
      ;;
    logs)
      compose_logs
      ;;
    *)
      usage
      die "Unknown action: ${ACTION}"
      ;;
  esac
}

main
