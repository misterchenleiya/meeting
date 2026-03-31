#!/usr/bin/env bash
set -euo pipefail

_now() { date '+%Y-%m-%d %H:%M:%S'; }
log() { echo "[INFO][$(_now)] $*"; }
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
BACKEND_BINARY="${SERVICE_DIR}/backend/meeting"
FRONTEND_INDEX="${SERVICE_DIR}/frontend/dist/index.html"
FRONTEND_CONF="${SERVICE_DIR}/frontend/nginx.conf"
COTURN_CONF="${SERVICE_DIR}/coturn/turnserver.conf"

command -v docker >/dev/null 2>&1 || fail "docker command not found"
[ -f "${COMPOSE_FILE}" ] || fail "docker-compose file not found: ${COMPOSE_FILE}"
[ -x "${BACKEND_BINARY}" ] || fail "backend binary not executable: ${BACKEND_BINARY}"
[ -f "${FRONTEND_INDEX}" ] || fail "frontend dist not found: ${FRONTEND_INDEX}"
[ -f "${FRONTEND_CONF}" ] || fail "frontend nginx config not found: ${FRONTEND_CONF}"
[ -f "${COTURN_CONF}" ] || fail "coturn config not found: ${COTURN_CONF}"

mkdir -p "${SERVICE_DIR}/logs" "${SERVICE_DIR}/data"

compose=()
if docker compose version >/dev/null 2>&1; then
  compose=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  compose=(docker-compose)
else
  fail "docker compose command not found"
fi

log "starting docker stack: meeting-backend meeting-frontend meeting-coturn"
"${compose[@]}" -f "${COMPOSE_FILE}" -p "${COMPOSE_PROJECT_NAME}" up -d --remove-orphans
log "docker stack started"
