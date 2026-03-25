#!/bin/zsh
set -euo pipefail

ROOT="$(cd -- "$(dirname "$0")" && pwd)"
CHROME="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
MODE="${1:-all}"
COMMON_FLAGS=(
  --headless=new
  --no-sandbox
  --disable-gpu
  --hide-scrollbars
  --allow-file-access-from-files
  --disable-background-networking
  --disable-component-update
  --disable-default-apps
  --disable-sync
  --metrics-recording-only
  --no-first-run
  --no-default-browser-check
)

if [[ ! -x "${CHROME}" ]]; then
  echo "Chrome executable not found: ${CHROME}" >&2
  echo "Set CHROME=/absolute/path/to/chrome and retry." >&2
  exit 1
fi

render_page() {
  local page_name="$1"
  local viewport="$2"
  local output_name="$3"
  local profile_name="$4"
  local output_path="${ROOT}/${output_name}"
  local log_path="/tmp/${profile_name}.log"
  local previous_mtime=0
  local chrome_pid=""
  local attempts=0

  if [[ -e "${output_path}" ]]; then
    previous_mtime="$(stat -f %m "${output_path}")"
  fi

  "${CHROME}" \
    "${COMMON_FLAGS[@]}" \
    --user-data-dir="/tmp/${profile_name}" \
    --screenshot="${output_path}" \
    --window-size="${viewport}" \
    "file://${ROOT}/${page_name}" \
    >"${log_path}" 2>&1 &
  chrome_pid=$!

  while kill -0 "${chrome_pid}" 2>/dev/null; do
    if [[ -s "${output_path}" ]]; then
      if [[ "$(stat -f %m "${output_path}")" != "${previous_mtime}" ]]; then
        sleep 1
        break
      fi
    fi

    attempts=$((attempts + 1))
    if (( attempts >= 60 )); then
      kill "${chrome_pid}" 2>/dev/null || true
      wait "${chrome_pid}" 2>/dev/null || true
      cat "${log_path}" >&2
      echo "Render timed out for ${output_name}" >&2
      return 1
    fi

    sleep 1
  done

  if kill -0 "${chrome_pid}" 2>/dev/null; then
    kill "${chrome_pid}" 2>/dev/null || true
  fi
  wait "${chrome_pid}" 2>/dev/null || true

  if [[ ! -s "${output_path}" ]]; then
    cat "${log_path}" >&2
    echo "Render failed for ${output_name}" >&2
    return 1
  fi
}

render_console_desktop() {
  render_page "meeting-console-preview.html" "1440,4400" "meeting-console-preview-desktop.png" "meeting-preview-console-desktop"
}

render_console_mobile() {
  render_page "meeting-console-preview.html" "430,5600" "meeting-console-preview-mobile.png" "meeting-preview-console-mobile"
}

render_room_desktop() {
  render_page "meeting-room-preview.html" "1440,2200" "meeting-room-preview-desktop.png" "meeting-preview-room-desktop"
}

render_room_mobile() {
  render_page "meeting-room-preview.html" "430,3200" "meeting-room-preview-mobile.png" "meeting-preview-room-mobile"
}

render_entry_desktop() {
  render_page "meeting-entry-preview.html" "1440,2600" "meeting-entry-preview-desktop.png" "meeting-preview-entry-desktop"
}

render_entry_mobile() {
  render_page "meeting-entry-preview.html" "430,3000" "meeting-entry-preview-mobile.png" "meeting-preview-entry-mobile"
}

render_auth_desktop() {
  render_page "meeting-auth-preview.html" "1440,1800" "meeting-auth-preview-desktop.png" "meeting-preview-auth-desktop"
}

render_auth_mobile() {
  render_page "meeting-auth-preview.html" "430,2600" "meeting-auth-preview-mobile.png" "meeting-preview-auth-mobile"
}

render_host_login_preview() {
  render_page "meeting-auth-preview.html#login" "1440,1800" "meeting-host-login-preview.png" "meeting-preview-host-login"
}

render_host_create_preview() {
  render_page "meeting-auth-preview.html#schedule" "1440,1800" "meeting-host-create-preview.png" "meeting-preview-host-create"
}

render_host_room_preview() {
  render_page "meeting-room-preview.html" "1440,2200" "meeting-host-room-preview.png" "meeting-preview-host-room"
}

case "${MODE}" in
  all)
    render_auth_desktop
    render_auth_mobile
    render_room_desktop
    render_room_mobile
    render_host_login_preview
    render_host_create_preview
    render_host_room_preview
    render_console_desktop
    render_console_mobile
    render_entry_desktop
    render_entry_mobile
    ;;
  host-flow)
    render_host_login_preview
    render_host_create_preview
    render_host_room_preview
    ;;
  auth)
    render_auth_desktop
    render_auth_mobile
    ;;
  room)
    render_room_desktop
    render_room_mobile
    ;;
  console)
    render_console_desktop
    render_console_mobile
    ;;
  entry)
    render_entry_desktop
    render_entry_mobile
    ;;
  desktop)
    render_auth_desktop
    render_room_desktop
    render_console_desktop
    render_entry_desktop
    ;;
  mobile)
    render_auth_mobile
    render_room_mobile
    render_console_mobile
    render_entry_mobile
    ;;
  auth-desktop)
    render_auth_desktop
    ;;
  auth-mobile)
    render_auth_mobile
    ;;
  host-login)
    render_host_login_preview
    ;;
  host-create)
    render_host_create_preview
    ;;
  host-room)
    render_host_room_preview
    ;;
  room-desktop)
    render_room_desktop
    ;;
  room-mobile)
    render_room_mobile
    ;;
  console-desktop)
    render_console_desktop
    ;;
  console-mobile)
    render_console_mobile
    ;;
  entry-desktop)
    render_entry_desktop
    ;;
  entry-mobile)
    render_entry_mobile
    ;;
  *)
    echo "Unsupported mode: ${MODE}" >&2
    echo "Supported modes: all, host-flow, host-login, host-create, host-room, auth, room, console, entry, desktop, mobile, auth-desktop, auth-mobile, room-desktop, room-mobile, console-desktop, console-mobile, entry-desktop, entry-mobile" >&2
    exit 1
    ;;
esac
