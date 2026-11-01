#!/bin/sh
set -eu

COMMAND_LABEL=dev.nils-silbernagel.fs-clip
GOOS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$GOOS" in
  darwin) STATE_DIR="$HOME/Library/Application Support/fs-clip" ;;
  linux) STATE_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/fs-clip" ;;
  *) echo 'Unsupported platform' >&2; exit 1 ;;
esac

usage() {
  echo 'Usage: manage.sh configure --watch-dir DIRECTORY | status | uninstall'
  exit 2
}

load_settings() {
  if [ ! -f "$STATE_DIR/install-dir" ] || [ ! -f "$STATE_DIR/watch-dir" ]; then
    echo 'No script-managed installation found. Run the installer first.' >&2
    exit 1
  fi
  IFS= read -r BINDIR < "$STATE_DIR/install-dir"
  IFS= read -r WATCH_DIR < "$STATE_DIR/watch-dir"
  case "$BINDIR" in /*) ;; *) echo 'Invalid installation path' >&2; exit 1 ;; esac
  case "$WATCH_DIR" in /*) ;; *) echo 'Invalid watch directory' >&2; exit 1 ;; esac
}

save_settings() {
  printf '%s\n' "$BINDIR" > "$STATE_DIR/install-dir.new"
  printf '%s\n' "$WATCH_DIR" > "$STATE_DIR/watch-dir.new"
  mv "$STATE_DIR/install-dir.new" "$STATE_DIR/install-dir"
  mv "$STATE_DIR/watch-dir.new" "$STATE_DIR/watch-dir"
}

activate() {
  mkdir -p "$WATCH_DIR"
  case "$GOOS" in
    darwin) install_mac ;;
    linux) install_linux ;;
  esac
}

escape_sed() {
  printf '%s' "${1}" | sed 's/[\\&|]/\\&/g'
}

escape_xml() {
  printf '%s' "${1}" | sed 's/\&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g'
}

escape_systemd() {
  printf '%s' "${1}" | sed 's/[\\"]/\\&/g; s/%/%%/g'
}

install_linux() {
  SYSTEMD_DEFINITIONS="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
  SERVICE="${COMMAND_LABEL}.service"

  echo "Copying service to ${SYSTEMD_DEFINITIONS}"
  mkdir -p "${SYSTEMD_DEFINITIONS}"
  sed "s|{WATCH_DIR}|$(escape_sed "$(escape_systemd "${WATCH_DIR}")")|g" "${STATE_DIR}/${SERVICE}" \
    | sed "s|{BINARY_PATH}|$(escape_sed "$(escape_systemd "${BINDIR}/fs-clip")")|g" \
    > "${SYSTEMD_DEFINITIONS}/${SERVICE}"
  chmod 0644 "${SYSTEMD_DEFINITIONS}/${SERVICE}"

  echo "Unloading systemd service"
  if systemctl --user cat "${SERVICE}" >/dev/null 2>&1; then
    echo "Unloading current instance of ${SERVICE}"
    systemctl --user disable --now "${SERVICE}"
  fi

  echo "Loading systemd service"
  systemctl --user daemon-reload
  systemctl --user enable --now "${SERVICE}"
}

install_mac() {
  LAUNCHAGENTS="${HOME}/Library/LaunchAgents"
  LOGDIR="${HOME}/Library/Logs/fs-clip"
  PLIST="${COMMAND_LABEL}.plist"

  mkdir -p "${LAUNCHAGENTS}" "${LOGDIR}"

  echo "Copying plist to ${LAUNCHAGENTS}"
  sed "s|{USER_HOME}|$(escape_sed "$(escape_xml "${HOME}")")|g" "${STATE_DIR}/${PLIST}" \
    | sed "s|{WATCH_DIR}|$(escape_sed "$(escape_xml "${WATCH_DIR}")")|g" \
    | sed "s|{BINARY_PATH}|$(escape_sed "$(escape_xml "${BINDIR}/fs-clip")")|g" \
    > "${LAUNCHAGENTS}/${PLIST}"
  chmod 0644 "${LAUNCHAGENTS}/${PLIST}"

  echo "Unloading launchd plist"
  if launchctl print "gui/$(id -u)/${COMMAND_LABEL}" >/dev/null 2>&1; then
    echo "Unloading current instance of ${PLIST}"
    launchctl bootout "gui/$(id -u)/${COMMAND_LABEL}"
  fi

  echo "Loading launchd plist"
  launchctl bootstrap "gui/$(id -u)" "${LAUNCHAGENTS}/${PLIST}"
}

mode=${1:-}
[ "$#" -gt 0 ] && shift
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"
if [ "${FS_CLIP_INSTALL_LOCK:-0}" != 1 ]; then
  if ! mkdir "$STATE_DIR/lock" 2>/dev/null; then
    echo "Another installation or management operation is running (lock: $STATE_DIR/lock)." >&2
    exit 1
  fi
  trap 'rmdir "$STATE_DIR/lock"' EXIT
fi
trap 'exit 1' INT TERM

case "$mode" in
  setup)
    [ "$#" -eq 2 ] || usage
    BINDIR=$1
    WATCH_DIR=$2
    activate
    save_settings
    ;;
  configure)
    [ "$#" -eq 2 ] && [ "$1" = '--watch-dir' ] || usage
    load_settings
    previous=$WATCH_DIR
    case "$2" in
      /*) WATCH_DIR=$2 ;;
      *) WATCH_DIR="$PWD/$2" ;;
    esac
    case "$WATCH_DIR" in *'
'*) echo 'Paths containing newlines are unsupported' >&2; exit 1 ;; esac
    # A subshell preserves errexit: a failed service command must abort activation.
    set +e
    (set -e; activate)
    result=$?
    set -e
    if [ "$result" -ne 0 ]; then
      WATCH_DIR=$previous
      echo 'Configuration failed; restoring the previous service configuration.' >&2
      activate
      exit "$result"
    fi
    save_settings
    ;;
  status)
    [ "$#" -eq 0 ] || usage
    load_settings
    printf 'Executable: %s/fs-clip\nWatching: %s\n' "$BINDIR" "$WATCH_DIR"
    case "$GOOS" in
      darwin) launchctl print "gui/$(id -u)/$COMMAND_LABEL" ;;
      linux) systemctl --user status "$COMMAND_LABEL.service" ;;
    esac
    ;;
  uninstall)
    [ "$#" -eq 0 ] || usage
    if [ ! -f "$STATE_DIR/install-dir" ]; then
      echo 'FSClip is already uninstalled.'
      exit 0
    fi
    load_settings
    case "$GOOS" in
      darwin)
        if launchctl print "gui/$(id -u)/$COMMAND_LABEL" >/dev/null 2>&1; then
          launchctl bootout "gui/$(id -u)/$COMMAND_LABEL"
        fi
        rm -f "$HOME/Library/LaunchAgents/$COMMAND_LABEL.plist"
        ;;
      linux)
        if systemctl --user cat "$COMMAND_LABEL.service" >/dev/null 2>&1; then
          systemctl --user disable --now "$COMMAND_LABEL.service"
        fi
        rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$COMMAND_LABEL.service"
        systemctl --user daemon-reload
        ;;
    esac
    rm -f "$BINDIR/fs-clip" "$STATE_DIR/install-dir"
    echo 'FSClip uninstalled. Watched files, preferences, and logs were preserved.'
    ;;
  *) usage ;;
esac
