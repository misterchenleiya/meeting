#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
STOP_SCRIPT="${SCRIPT_DIR}/stop.sh"
START_SCRIPT="${SCRIPT_DIR}/start.sh"

[ -x "${STOP_SCRIPT}" ] || { echo "stop.sh not executable: ${STOP_SCRIPT}" >&2; exit 1; }
[ -x "${START_SCRIPT}" ] || { echo "start.sh not executable: ${START_SCRIPT}" >&2; exit 1; }

"${STOP_SCRIPT}" --remove
"${START_SCRIPT}"

