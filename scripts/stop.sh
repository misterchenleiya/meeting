#!/usr/bin/env bash
set -euo pipefail

_now() { date '+%Y-%m-%d %H:%M:%S'; }
log() { echo "[INFO][$(_now)] $*"; }
warn() { echo "[WARN][$(_now)] $*" >&2; }
fail() { echo "[ERROR][$(_now)] $*" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
resolve_service_dir() {
  if [[ -f "${SCRIPT_DIR}/docker-compose.yml" ]]; then
    printf '%s\n' "${SCRIPT_DIR}"
    return
  fi

  if [[ -f "${SCRIPT_DIR}/../docker-compose.yml" ]]; then
    cd "${SCRIPT_DIR}/.." && pwd -P
    return
  fi

  printf '%s\n' "${SCRIPT_DIR}"
}

SERVICE_DIR="${SERVICE_DIR:-$(resolve_service_dir)}"
SERVICE_DIR="$(cd "${SERVICE_DIR}" && pwd -P)"
COMPOSE_FILE="${COMPOSE_FILE:-${SERVICE_DIR}/docker-compose.yml}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-meeting}"
REMOVE=1

case "${1:-}" in
  ""|--remove)
    REMOVE=1
    ;;
  --keep)
    REMOVE=0
    ;;
  *)
    fail "unsupported argument: ${1} (use --keep or --remove)"
    ;;
esac

command -v docker >/dev/null 2>&1 || fail "docker command not found"
[ -f "${COMPOSE_FILE}" ] || fail "docker-compose file not found: ${COMPOSE_FILE}"

compose=()
if docker compose version >/dev/null 2>&1; then
  compose=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  compose=(docker-compose)
else
  fail "docker compose command not found"
fi

if ! "${compose[@]}" -f "${COMPOSE_FILE}" -p "${COMPOSE_PROJECT_NAME}" ps >/dev/null 2>&1; then
  warn "docker stack is not running: ${COMPOSE_PROJECT_NAME}"
fi

if [ "${REMOVE}" = "1" ]; then
  log "stopping and removing docker stack"
  "${compose[@]}" -f "${COMPOSE_FILE}" -p "${COMPOSE_PROJECT_NAME}" down --remove-orphans
else
  log "stopping docker stack"
  "${compose[@]}" -f "${COMPOSE_FILE}" -p "${COMPOSE_PROJECT_NAME}" stop
fi

log "docker stack stopped"
