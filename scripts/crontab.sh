#!/usr/bin/env bash
set -euo pipefail

SERVICE_DIR="${SERVICE_DIR:-$(pwd -P)}"
SERVICE_NAME="${SERVICE_NAME:-$(basename "${SERVICE_DIR}")}"
UPDATE_SCRIPT="${UPDATE_SCRIPT:-${SERVICE_DIR}/update.sh}"
LOG_DIR="${LOG_DIR:-${SERVICE_DIR}/logs}"
LOG_SYMLINK="${LOG_DIR}/crontab.log"
LATEST_URL="${LATEST_URL:-https://download.07c2.com/${SERVICE_NAME}/latest.txt}"
MEETING_BACKEND_ENV_FILE="${MEETING_BACKEND_ENV_FILE:-}"
DEFAULT_SCHEDULE="${DEFAULT_SCHEDULE:-* * * * *}"
CRON_BEGIN="# >>> ${SERVICE_NAME}-update begin >>>"
CRON_END="# <<< ${SERVICE_NAME}-update end <<<"
TMP_FILES=()
RUN_ID=""
LOG_DATE=""
LOG_FILE=""

register_tmp_file() {
  TMP_FILES+=("$1")
}

cleanup_tmp_files() {
  local tmp_file

  for tmp_file in "${TMP_FILES[@]}"; do
    [[ -n "${tmp_file}" ]] || continue
    rm -f "${tmp_file}" "${tmp_file}.clean" "${tmp_file}.new"
  done
}

trap cleanup_tmp_files EXIT

json_escape() {
  local value="${1-}"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\t'/\\t}"
  printf '%s' "${value}"
}

log_json() {
  local level="${1,,}"
  local message="$2"
  shift 2

  [[ -n "${LOG_FILE}" ]] || return 0

  local line
  line="{\"level\":\"$(json_escape "${level}")\",\"time\":\"$(date '+%Y-%m-%dT%H:%M:%S%z')\",\"message\":\"$(json_escape "${message}")\",\"script\":\"crontab.sh\""
  line+=",\"run_id\":\"$(json_escape "${RUN_ID}")\""
  line+=",\"service\":\"$(json_escape "${SERVICE_NAME}")\""
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

log_info() {
  log_json info "$@"
}

log_error() {
  log_json error "$@"
}

usage() {
  cat <<EOF
用法：
  ./crontab.sh add [schedule]
  ./crontab.sh check
  ./crontab.sh stop

说明：
  add [schedule]  安装或更新当前服务的 crontab，默认 "${DEFAULT_SCHEDULE}"
  check           检查当前服务的 crontab 与日志目录
  stop            删除当前服务的 crontab 托管块

默认上下文：
  - 服务名称：${SERVICE_NAME}
  - 服务目录：${SERVICE_DIR}
  - update 脚本：${UPDATE_SCRIPT}
  - 下载路径：${LATEST_URL}
EOF
}

escape_single_quotes() {
  printf "%s" "$1" | sed "s/'/'\"'\"'/g"
}

latest_log_file() {
  if [[ ! -d "${LOG_DIR}" ]]; then
    return
  fi

  ls -1t "${LOG_DIR}"/*_update.log 2>/dev/null | head -n 1
}

rotate_logs() {
  local keep_days day log_file
  keep_days="$(
    find "${LOG_DIR}" -maxdepth 1 -type f -name '*_crontab.log' -printf '%f\n' 2>/dev/null | \
      awk -F_ '{print $1}' | sort -r | uniq | head -n 3
  )"

  while IFS= read -r log_file; do
    [[ -n "${log_file}" ]] || continue
    day="${log_file%%_*}"
    if ! printf '%s\n' "${keep_days}" | grep -Fxq "${day}"; then
      rm -f "${LOG_DIR}/${log_file}"
    fi
  done < <(find "${LOG_DIR}" -maxdepth 1 -type f -name '*_crontab.log' -printf '%f\n' 2>/dev/null | sort)
}

setup_logging() {
  mkdir -p "${LOG_DIR}"
  RUN_ID="$(date '+%Y-%m-%d_%H%M%S')_$$"
  LOG_DATE="$(date '+%Y-%m-%d')"
  LOG_FILE="${LOG_DIR}/${LOG_DATE}_crontab.log"
  touch "${LOG_FILE}"
  ln -sfn "${LOG_FILE}" "${LOG_SYMLINK}"
}

build_cron_command() {
  local schedule="$1"
  local escaped_service_dir
  local escaped_backend_env_file
  escaped_service_dir="$(escape_single_quotes "${SERVICE_DIR}")"
  if [[ -n "${MEETING_BACKEND_ENV_FILE}" ]]; then
    escaped_backend_env_file="$(escape_single_quotes "${MEETING_BACKEND_ENV_FILE}")"
    printf "%s /bin/bash -lc 'cd '\''%s'\'' && export MEETING_BACKEND_ENV_FILE='\''%s'\'' && ./update.sh >/dev/null 2>&1'" \
      "${schedule}" "${escaped_service_dir}" "${escaped_backend_env_file}"
    return
  fi

  printf "%s /bin/bash -lc 'cd '\''%s'\'' && ./update.sh >/dev/null 2>&1'" "${schedule}" "${escaped_service_dir}"
}

cmd_add() {
  local schedule="${1:-${DEFAULT_SCHEDULE}}"
  local cron_cmd
  local tmp_file

  [[ -f "${UPDATE_SCRIPT}" ]] || {
    echo "update 脚本不存在：${UPDATE_SCRIPT}" >&2
    exit 1
  }

  mkdir -p "${LOG_DIR}"
  cron_cmd="$(build_cron_command "${schedule}")"
  tmp_file="$(mktemp)"
  register_tmp_file "${tmp_file}"

  if crontab -l > "${tmp_file}" 2>/dev/null; then
    :
  else
    : > "${tmp_file}"
  fi

  awk -v begin="${CRON_BEGIN}" -v end="${CRON_END}" '
    $0 == begin {skip=1; next}
    $0 == end {skip=0; next}
    !skip {print}
  ' "${tmp_file}" > "${tmp_file}.clean"
  mv "${tmp_file}.clean" "${tmp_file}"

  {
    cat "${tmp_file}"
    echo "${CRON_BEGIN}"
    echo "${cron_cmd}"
    echo "${CRON_END}"
  } > "${tmp_file}.new"

  crontab "${tmp_file}.new"
  log_info "service crontab installed" schedule="${schedule}"
  echo "${SERVICE_NAME} crontab 已安装，调度为: ${schedule}"
}

cmd_check() {
  local tmp_cron_file
  local latest_log=""
  local cron_status="not_installed"
  tmp_cron_file="$(mktemp)"
  register_tmp_file "${tmp_cron_file}"

  echo "service: ${SERVICE_NAME}"
  echo "service dir: ${SERVICE_DIR}"
  echo "update script: ${UPDATE_SCRIPT}"
  echo "download latest: ${LATEST_URL}"
  echo "log dir: ${LOG_DIR}"
  if [[ -n "${MEETING_BACKEND_ENV_FILE}" ]]; then
    echo "backend env file: ${MEETING_BACKEND_ENV_FILE}"
  else
    echo "backend env file: (use compose default /etc/meeting/meeting-backend.env)"
  fi

  if crontab -l 2>/dev/null | awk -v begin="${CRON_BEGIN}" -v end="${CRON_END}" '
    $0 == begin {in_block=1; next}
    $0 == end {in_block=0; exit}
    in_block {print}
  ' | sed '/^[[:space:]]*$/d' > "${tmp_cron_file}"; then
    if [[ -s "${tmp_cron_file}" ]]; then
      cron_status="installed"
      echo "crontab: installed"
      cat "${tmp_cron_file}"
    else
      echo "crontab: not installed"
    fi
  else
    echo "crontab: not installed"
  fi

  latest_log="$(latest_log_file || true)"
  if [[ -n "${latest_log}" ]]; then
    echo "latest log: ${latest_log}"
  fi

  log_info "service crontab check completed" crontab="${cron_status}" latest_log="${latest_log}"
}

cmd_stop() {
  local tmp_file
  tmp_file="$(mktemp)"
  register_tmp_file "${tmp_file}"

  if crontab -l > "${tmp_file}" 2>/dev/null; then
    :
  else
    echo "${SERVICE_NAME} crontab 未安装"
    exit 0
  fi

  awk -v begin="${CRON_BEGIN}" -v end="${CRON_END}" '
    $0 == begin {skip=1; found=1; next}
    $0 == end {skip=0; next}
    !skip {print}
    END {
      if (found != 1) {
        exit 3
      }
    }
  ' "${tmp_file}" > "${tmp_file}.clean" || {
    echo "${SERVICE_NAME} crontab 未安装"
    exit 0
  }

  crontab "${tmp_file}.clean"
  log_info "service crontab removed"
  echo "${SERVICE_NAME} crontab 已删除"
}

main() {
  local command="${1:-}"

  setup_logging
  rotate_logs
  log_info "crontab command started" command="${command:-help}"

  case "${command}" in
    add)
      shift
      cmd_add "${1:-}"
      ;;
    check)
      shift
      cmd_check
      ;;
    stop)
      shift
      cmd_stop
      ;;
    ""|-h|--help|help)
      usage
      ;;
    *)
      log_error "unknown crontab command" command="${command}"
      echo "未知命令：${command}" >&2
      usage >&2
      exit 1
      ;;
  esac
}

main "$@"
