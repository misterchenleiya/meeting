#!/usr/bin/env bash
set -euo pipefail

_now() { date '+%Y-%m-%d %H:%M:%S'; }
log() { echo "[INFO][$(_now)] $*"; }
fail() { echo "[ERROR][$(_now)] $*" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
SERVICE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd -P)"
LOG_DIR="${LOG_DIR:-${SERVICE_DIR}/logs}"
LOG_FILE="${LOG_FILE:-}"

command -v tail >/dev/null 2>&1 || fail "tail command not found"

if [ -z "${LOG_FILE}" ]; then
  LOG_FILE="$(ls -1t "${LOG_DIR}"/*_meeting.log 2>/dev/null | head -n 1 || true)"
fi

[ -n "${LOG_FILE}" ] || fail "backend log not found in ${LOG_DIR}"
[ -f "${LOG_FILE}" ] || fail "backend log does not exist: ${LOG_FILE}"

log "following backend log: ${LOG_FILE}"
tail -F "${LOG_FILE}"

