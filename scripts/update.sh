#!/usr/bin/env bash
set -euo pipefail

PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

_now() { date '+%Y-%m-%d %H:%M:%S'; }
json_escape() {
  local value="${1-}"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\t'/\\t}"
  printf '%s' "${value}"
}

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

resolve_runtime_script_path() {
  local script_name="$1"

  if [[ -f "${SERVICE_DIR}/${script_name}" ]]; then
    printf '%s\n' "${SERVICE_DIR}/${script_name}"
    return 0
  fi

  if [[ -f "${SERVICE_DIR}/scripts/${script_name}" ]]; then
    printf '%s\n' "${SERVICE_DIR}/scripts/${script_name}"
    return 0
  fi

  return 1
}

SERVICE_DIR="${SERVICE_DIR:-$(resolve_service_dir)}"
SERVICE_DIR="$(cd "${SERVICE_DIR}" && pwd -P)"
PROJECT_NAME="${PROJECT_NAME:-meeting}"
DOWNLOAD_BASE_URL="${DOWNLOAD_BASE_URL:-https://download.07c2.com/${PROJECT_NAME}}"
LATEST_URL="${LATEST_URL:-${DOWNLOAD_BASE_URL}/latest.txt}"
LOG_DIR="${LOG_DIR:-${SERVICE_DIR}/logs}"
LOG_SYMLINK="${LOG_SYMLINK:-${LOG_DIR}/update.log}"
LATEST_LOCAL_FILE="${LATEST_LOCAL_FILE:-${SERVICE_DIR}/latest.txt}"
CURRENT_FILE="${CURRENT_FILE:-${SERVICE_DIR}/current.txt}"
ARCHIVE_NAME=""
ARCHIVE_PATH=""
ARCHIVE_TMP_PATH=""
RUN_ID="$(date '+%Y-%m-%d_%H%M%S')_$$"
LOG_DATE="$(date '+%Y-%m-%d')"
LOG_FILE="${LOG_DIR}/${LOG_DATE}_update.log"
LOCK_DIR="${LOG_DIR}/update.lock"

mkdir -p "${LOG_DIR}"
touch "${LOG_FILE}"
ln -sfn "${LOG_FILE}" "${LOG_SYMLINK}"

log_json() {
  local level="${1,,}"
  local message="$2"
  shift 2

  local line
  line="{\"level\":\"$(json_escape "${level}")\",\"time\":\"$(date '+%Y-%m-%dT%H:%M:%S%z')\",\"message\":\"$(json_escape "${message}")\""
  line+=",\"run_id\":\"$(json_escape "${RUN_ID}")\""
  line+=",\"project\":\"$(json_escape "${PROJECT_NAME}")\""
  line+=",\"directory\":\"$(json_escape "${SERVICE_DIR}")\""

  local kv key value
  for kv in "$@"; do
    key="${kv%%=*}"
    value="${kv#*=}"
    [[ -n "${key}" ]] || continue
    line+=",\"$(json_escape "${key}")\":\"$(json_escape "${value}")\""
  done

  line+='}'
  printf '%s\n' "${line}" >> "${LOG_FILE}"
}

log_info() { log_json info "$@"; }
log_warn() { log_json warn "$@"; }
log_error() { log_json error "$@"; }

ensure_command() {
  local command_name="$1"
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    log_error "missing required command" command="${command_name}"
    exit 1
  fi
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi

  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi

  log_error "no sha256 tool found"
  exit 1
}

download_text() {
  local url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 15 --max-time 120 "${url}"
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -q -T 15 -O - "${url}"
    return
  fi

  log_error "no downloader available" url="${url}"
  exit 1
}

download_file() {
  local url="$1"
  local out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 15 --max-time 300 -o "${out}" "${url}"
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -q -T 15 -O "${out}" "${url}"
    return
  fi

  log_error "no downloader available" url="${url}"
  exit 1
}

with_update_lock() {
  if mkdir "${LOCK_DIR}" 2>/dev/null; then
    trap 'rmdir "${LOCK_DIR}" >/dev/null 2>&1 || true' EXIT INT TERM HUP
    return 0
  fi

  log_info "another update is already running"
  exit 0
}

parse_field() {
  local content="$1"
  local key="$2"
  printf '%s\n' "${content}" | sed -n "s/^${key}:[[:space:]]*//p" | head -n 1 | tr -d '\r'
}

cleanup_old_archives() {
  local current_archive="$1"
  local file
  while IFS= read -r file; do
    [[ -n "${file}" ]] || continue
    if [[ "${file}" != "${current_archive}" ]]; then
      rm -f "${SERVICE_DIR}/${file}"
    fi
  done < <(find "${SERVICE_DIR}" -maxdepth 1 -type f -name "${PROJECT_NAME}_*.tar.gz" 2>/dev/null | sed 's#.*/##' | sort)
}

ensure_command tar
ensure_command find
ensure_command sed
ensure_command awk
ensure_command rm
ensure_command mkdir
ensure_command cp
ensure_command mv

with_update_lock
log_info "update started" latest_url="${LATEST_URL}"

LATEST_REMOTE_TEXT="$(download_text "${LATEST_URL}")"
ARCHIVE_NAME="$(parse_field "${LATEST_REMOTE_TEXT}" filename)"
EXPECTED_SHA256="$(parse_field "${LATEST_REMOTE_TEXT}" sha256sum)"

if [[ -z "${ARCHIVE_NAME}" ]]; then
  log_error "latest.txt missing filename field" latest_url="${LATEST_URL}"
  exit 1
fi

if [[ -z "${EXPECTED_SHA256}" ]]; then
  log_error "latest.txt missing sha256sum field" latest_url="${LATEST_URL}"
  exit 1
fi

case "${ARCHIVE_NAME}" in
  *.tar.gz) ;;
  *)
    log_error "unsupported package filename" filename="${ARCHIVE_NAME}"
    exit 1
    ;;
esac

ARCHIVE_PATH="${SERVICE_DIR}/${ARCHIVE_NAME}"
ARCHIVE_TMP_PATH="${ARCHIVE_PATH}.part"

printf '%s\n' "${LATEST_REMOTE_TEXT}" > "${LATEST_LOCAL_FILE}"

CURRENT_VERSION=""
if [[ -f "${CURRENT_FILE}" ]]; then
  CURRENT_VERSION="$(tr -d '\r\n' < "${CURRENT_FILE}")"
fi

if [[ "${CURRENT_VERSION}" == "${ARCHIVE_NAME}" ]] && [[ -f "${ARCHIVE_PATH}" ]]; then
  CURRENT_SHA256="$(sha256_file "${ARCHIVE_PATH}")"
  if [[ "${CURRENT_SHA256}" == "${EXPECTED_SHA256}" ]]; then
    log_info "already up to date" filename="${ARCHIVE_NAME}" sha256="${EXPECTED_SHA256}"
    exit 0
  fi
fi

if [[ -f "${ARCHIVE_PATH}" ]]; then
  CURRENT_SHA256="$(sha256_file "${ARCHIVE_PATH}")"
  if [[ "${CURRENT_SHA256}" != "${EXPECTED_SHA256}" ]]; then
    rm -f "${ARCHIVE_PATH}"
  fi
fi

if [[ ! -f "${ARCHIVE_PATH}" ]]; then
  log_info "downloading archive" url="${DOWNLOAD_BASE_URL}/${ARCHIVE_NAME}"
  rm -f "${ARCHIVE_TMP_PATH}"
  download_file "${DOWNLOAD_BASE_URL}/${ARCHIVE_NAME}" "${ARCHIVE_TMP_PATH}"

  DOWNLOADED_SHA256="$(sha256_file "${ARCHIVE_TMP_PATH}")"
  if [[ "${DOWNLOADED_SHA256}" != "${EXPECTED_SHA256}" ]]; then
    rm -f "${ARCHIVE_TMP_PATH}"
    log_error "sha256 mismatch" filename="${ARCHIVE_NAME}" expected_sha256="${EXPECTED_SHA256}" actual_sha256="${DOWNLOADED_SHA256}"
    exit 1
  fi

  mv -f "${ARCHIVE_TMP_PATH}" "${ARCHIVE_PATH}"
  log_info "archive downloaded" filename="${ARCHIVE_NAME}" sha256="${EXPECTED_SHA256}"
fi

log_info "stopping docker stack before update"
"${SCRIPT_DIR}/stop.sh" --remove

log_info "extracting archive" archive="${ARCHIVE_PATH}"
tar -xzf "${ARCHIVE_PATH}" -C "${SERVICE_DIR}"

if [[ ! -f "${SERVICE_DIR}/docker-compose.yml" ]]; then
  log_error "docker-compose.yml not found after extraction" path="${SERVICE_DIR}/docker-compose.yml"
  exit 1
fi

START_SCRIPT_PATH="$(resolve_runtime_script_path start.sh || true)"
if [[ -z "${START_SCRIPT_PATH}" ]]; then
  log_error "start script not found after extraction" path="${SERVICE_DIR}/start.sh"
  exit 1
fi

if [[ ! -f "${SERVICE_DIR}/backend/meeting" ]]; then
  log_error "backend binary not found after extraction" path="${SERVICE_DIR}/backend/meeting"
  exit 1
fi

chmod +x "${SERVICE_DIR}/backend/meeting"
find "${SERVICE_DIR}" -maxdepth 1 -type f -name '*.sh' -exec chmod +x {} +
if [[ -d "${SERVICE_DIR}/scripts" ]]; then
  find "${SERVICE_DIR}/scripts" -maxdepth 1 -type f -name '*.sh' -exec chmod +x {} +
fi
mkdir -p "${SERVICE_DIR}/logs" "${SERVICE_DIR}/data"
printf '%s\n' "${ARCHIVE_NAME}" > "${CURRENT_FILE}"

log_info "starting docker stack after update"
"${START_SCRIPT_PATH}"

cleanup_old_archives "${ARCHIVE_NAME}"
log_info "update completed" filename="${ARCHIVE_NAME}" sha256="${EXPECTED_SHA256}" current="${CURRENT_FILE}"
