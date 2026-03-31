#!/usr/bin/env bash
set -euo pipefail

UPLOAD_BASE="${UPLOAD_BASE:-https://upload.07c2.com}"
UPLOAD_TIMEOUT_SECONDS="${UPLOAD_TIMEOUT_SECONDS:-30}"
UPLOAD_MAX_RETRIES="${UPLOAD_MAX_RETRIES:-5}"
UPLOAD_RETRY_DELAY_SECONDS="${UPLOAD_RETRY_DELAY_SECONDS:-1}"
DRY_RUN="${DRY_RUN:-0}"
SOURCE_PATH="${1:-}"
UPLOAD_PATH_RAW="${2:-}"

log_info() {
  printf '[INFO] %s\n' "$*" >&2
}

log_warn() {
  printf '[WARN] %s\n' "$*" >&2
}

log_error() {
  printf '[ERROR] %s\n' "$*" >&2
}

usage() {
  cat <<'EOF'
Usage:
  ./upload.sh /path/to/file /upload/path

Examples:
  ./upload.sh ./some_file.tar.gz /data
  ./upload.sh ./dist /data/releases

Behavior:
  - If /path/to/file is a file, upload it to https://upload.07c2.com/upload/path/<filename>
  - If /path/to/file is a directory, upload all files under it and preserve subdirectories
  - Existing files on upload.07c2.com are overwritten
EOF
}

require_command() {
  local command_name="$1"

  if ! command -v "${command_name}" >/dev/null 2>&1; then
    log_error "missing required command: ${command_name}"
    exit 1
  fi
}

normalize_upload_path() {
  local path="$1"

  if [[ -z "${path}" ]]; then
    log_error "upload path is empty"
    exit 1
  fi

  if [[ "${path}" != /* ]]; then
    path="/${path}"
  fi

  while [[ "${path}" != "/" && "${path}" == */ ]]; do
    path="${path%/}"
  done

  printf '%s\n' "${path}"
}

prompt_credentials_if_needed() {
  if [[ "${DRY_RUN}" == "1" ]]; then
    return
  fi

  if [[ -z "${deploy_username:-}" ]]; then
    printf 'deploy username: '
    IFS= read -r deploy_username
  fi

  if [[ -z "${deploy_username:-}" ]]; then
    log_error "deploy username is empty"
    exit 1
  fi

  if [[ -z "${deploy_password:-}" ]]; then
    printf 'deploy password: '
    trap 'stty echo' EXIT INT TERM
    stty -echo
    IFS= read -r deploy_password
    stty echo
    trap - EXIT INT TERM
    printf '\n'
  fi

  if [[ -z "${deploy_password:-}" ]]; then
    log_error "deploy password is empty"
    exit 1
  fi
}

build_destination_url() {
  local upload_root="$1"
  local relative_path="$2"

  if [[ "${upload_root}" == "/" ]]; then
    printf '%s/%s\n' "${UPLOAD_BASE}" "${relative_path}"
    return
  fi

  printf '%s%s/%s\n' "${UPLOAD_BASE}" "${upload_root}" "${relative_path}"
}

upload_file() {
  local src_path="$1"
  local dst_url="$2"
  local attempt

  if [[ "${DRY_RUN}" == "1" ]]; then
    log_info "dry-run upload ${src_path} -> ${dst_url}"
    return
  fi

  for ((attempt = 1; attempt <= UPLOAD_MAX_RETRIES; attempt++)); do
    if command -v pv >/dev/null 2>&1; then
      if pv -f "${src_path}" | curl \
        --fail \
        --show-error \
        --max-time "${UPLOAD_TIMEOUT_SECONDS}" \
        --user "${deploy_username}:${deploy_password}" \
        -T - \
        "${dst_url}"; then
        log_info "uploaded file=${src_path##*/} path=${dst_url} attempt=${attempt}"
        return
      fi
    elif curl \
      --fail \
      --show-error \
      --progress-bar \
      --stderr - \
      --max-time "${UPLOAD_TIMEOUT_SECONDS}" \
      --user "${deploy_username}:${deploy_password}" \
      -T "${src_path}" \
      "${dst_url}"; then
      log_info "uploaded file=${src_path##*/} path=${dst_url} attempt=${attempt}"
      return
    fi

    log_warn "upload failed (attempt ${attempt}/${UPLOAD_MAX_RETRIES}, timeout=${UPLOAD_TIMEOUT_SECONDS}s): ${dst_url}"
    if (( attempt < UPLOAD_MAX_RETRIES )); then
      sleep "${UPLOAD_RETRY_DELAY_SECONDS}"
    fi
  done

  log_error "upload failed after ${UPLOAD_MAX_RETRIES} attempts (timeout=${UPLOAD_TIMEOUT_SECONDS}s): ${dst_url}"
  exit 1
}

upload_single_file() {
  local src_path="$1"
  local upload_root="$2"
  local file_name dst_url

  file_name="$(basename "${src_path}")"
  dst_url="$(build_destination_url "${upload_root}" "${file_name}")"
  upload_file "${src_path}" "${dst_url}"
}

upload_directory() {
  local src_dir="$1"
  local upload_root="$2"
  local src_path rel_path dst_url found_any=0

  while IFS= read -r src_path; do
    [[ -n "${src_path}" ]] || continue
    found_any=1
    rel_path="${src_path#${src_dir}/}"
    dst_url="$(build_destination_url "${upload_root}" "${rel_path}")"
    upload_file "${src_path}" "${dst_url}"
  done < <(find "${src_dir}" -type f ! -name '.DS_Store' | sort)

  if [[ ${found_any} -eq 0 ]]; then
    log_warn "no files found under directory: ${src_dir}"
  fi
}

main() {
  local upload_root=""

  if [[ $# -lt 2 ]]; then
    usage
    exit 1
  fi

  require_command "curl"
  require_command "find"
  require_command "sort"

  if [[ ! -e "${SOURCE_PATH}" ]]; then
    log_error "source path does not exist: ${SOURCE_PATH}"
    exit 1
  fi

  upload_root="$(normalize_upload_path "${UPLOAD_PATH_RAW}")"

  prompt_credentials_if_needed

  if [[ -f "${SOURCE_PATH}" ]]; then
    upload_single_file "${SOURCE_PATH}" "${upload_root}"
    return
  fi

  if [[ -d "${SOURCE_PATH}" ]]; then
    upload_directory "${SOURCE_PATH}" "${upload_root}"
    return
  fi

  log_error "source path must be a file or directory: ${SOURCE_PATH}"
  exit 1
}

main "$@"
