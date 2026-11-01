#!/bin/sh

# fs-clip install script
# contains code from and inspired by
# https://github.com/client9/shlib
# https://github.com/twpayne/chezmoi/

set -e

BINDIR=""
TAGARG=latest
LOG_LEVEL=2
WATCH_DIR=""
RELEASE_BASE_URL="${FS_CLIP_RELEASE_BASE_URL:-https://github.com/N-Silbernagel/fs-clip/releases}"

tmpdir="$(mktemp -d)"
lock_owned=0
cleanup() {
  rm -rf -- "$tmpdir"
  if [ "$lock_owned" -eq 1 ]; then rmdir "$STATE_DIR/lock"; fi
}
trap cleanup EXIT
trap 'exit 1' INT TERM

usage() {
  this="${1}"
  cat <<EOF
${this}: download fs-clip and install it

Usage: ${this} [-d] [-b bindir] [-w watchdir] [-t tag]
  -d	enables debug logging.
  -b	specify install directory for the fs-clip binary.
  -w	specify custom watch directory.
  -t	sets the tag, default is ${TAGARG}.
EOF
  exit 2
}

main() {
  parse_args "${@}"
  shift "$((OPTIND - 1))"

  GOOS="$(get_goos)"
  GOARCH="$(get_goarch)"
  check_goos_goarch "${GOOS}/${GOARCH}"

  case "$GOOS" in
    darwin) STATE_DIR="$HOME/Library/Application Support/fs-clip" ;;
    linux) STATE_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/fs-clip" ;;
  esac
  mkdir -p "$STATE_DIR"
  chmod 700 "$STATE_DIR"
  if ! mkdir "$STATE_DIR/lock" 2>/dev/null; then
    log_err "another installation or management operation is running (lock: $STATE_DIR/lock)"
    exit 1
  fi
  lock_owned=1
  if [ -z "$BINDIR" ] && [ -f "$STATE_DIR/install-dir" ]; then
    IFS= read -r BINDIR < "$STATE_DIR/install-dir"
  fi
  if [ -z "$WATCH_DIR" ] && [ -f "$STATE_DIR/watch-dir" ]; then
    IFS= read -r WATCH_DIR < "$STATE_DIR/watch-dir"
  fi

  if [ -z "${BINDIR}" ]; then
    BINDIR="${HOME}/.local/bin"
  fi

  if [ -z "${WATCH_DIR}" ]; then
    WATCH_DIR="${HOME}/copy-to-clipboard"
  fi

  case "$BINDIR" in /*) ;; *) BINDIR="$PWD/$BINDIR" ;; esac
  case "$WATCH_DIR" in /*) ;; *) WATCH_DIR="$PWD/$WATCH_DIR" ;; esac
  case "$BINDIR$WATCH_DIR" in *'
'*) echo 'Paths containing newlines are unsupported' >&2; exit 1 ;; esac

  TAG="$(real_tag "${TAGARG}")"
  VERSION="${TAG#v}"

  BINSUFFIX=
  FORMAT=tar.gz
  case "${GOOS}" in
  windows)
    BINSUFFIX=.exe
    FORMAT=zip
    ;;
  esac

  arch="${GOARCH}"

  # download tarball
  NAME="fs-clip_${VERSION}_${GOOS}${GOOS_EXTRA}_${arch}"
  TARBALL="${NAME}.${FORMAT}"
  TARBALL_URL="${RELEASE_BASE_URL}/download/${TAG}/${TARBALL}"
  http_download "${tmpdir}/${TARBALL}" "${TARBALL_URL}" || exit 1

  # download checksums
  CHECKSUMS="fs-clip_${VERSION}_checksums.txt"
  CHECKSUMS_URL="${RELEASE_BASE_URL}/download/${TAG}/${CHECKSUMS}"
  if ! http_download "${tmpdir}/${CHECKSUMS}" "${CHECKSUMS_URL}"; then
    log_err "unable to download checksum manifest ${CHECKSUMS}"
    exit 1
  fi

  # verify checksums
  hash_sha256_verify "${tmpdir}/${TARBALL}" "${tmpdir}/${CHECKSUMS}"

  (cd -- "${tmpdir}" && untar "${TARBALL}")

  # Validate the management payload before replacing an existing executable.
  if [ ! -f "$tmpdir/manage.sh" ]; then
    log_err 'release archive is missing manage.sh; use the installer published with that release'
    return 1
  fi
  case "$GOOS" in
    darwin) template=dev.nils-silbernagel.fs-clip.plist ;;
    linux) template=dev.nils-silbernagel.fs-clip.service ;;
  esac
  if [ ! -f "$tmpdir/$template" ]; then
    log_err "release archive is missing $template"
    return 1
  fi
  sh -n "$tmpdir/manage.sh"
  if [ -f "$STATE_DIR/install-dir" ]; then
    IFS= read -r previous_bindir < "$STATE_DIR/install-dir"
    if [ "$previous_bindir" != "$BINDIR" ]; then
      log_err 'uninstall the existing installation before changing its binary directory'
      exit 1
    fi
  fi

  # install binary
  echo "Copying fs-clip to ${BINDIR}"
  if mkdir -p "${BINDIR}" 2>/dev/null && install -m 0755 "${tmpdir}/fs-clip" "${BINDIR}/.fs-clip-$$" 2>/dev/null && mv -f "${BINDIR}/.fs-clip-$$" "${BINDIR}/fs-clip" 2>/dev/null; then
    :
  else
    log_err "unable to install to ${BINDIR}; choose a writable path with -b"
    return 1
  fi

  install -m 0700 "${tmpdir}/manage.sh" "$STATE_DIR/manage.sh.new"
  mv -f "$STATE_DIR/manage.sh.new" "$STATE_DIR/manage.sh"
  install -m 0600 "$tmpdir/$template" "$STATE_DIR/$template.new"
  mv -f "$STATE_DIR/$template.new" "$STATE_DIR/$template"
  FS_CLIP_INSTALL_LOCK=1 sh "$STATE_DIR/manage.sh" setup "$BINDIR" "$WATCH_DIR"
  printf '\nInstalled FSClip. Management script: %s/manage.sh\n' "$STATE_DIR"
  printf 'Watching: %s\nUse the management script to configure, check status, or uninstall.\n' "$WATCH_DIR"
}

parse_args() {
  while getopts "b:w:dh?t:" arg; do
    case "${arg}" in
    b) BINDIR="${OPTARG}" ;;
    w) WATCH_DIR="${OPTARG}" ;;
    t) TAGARG="${OPTARG}" ;;
    d) LOG_LEVEL=3 ;;
    h | \?) usage "${0}" ;;
    *) return 1 ;;
    esac
  done
}

get_goos() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "${os}" in
  cygwin_nt*) goos="windows" ;;
  linux)
    case "$(uname -o | tr '[:upper:]' '[:lower:]')" in
    android) goos="android" ;;
    *) goos="linux" ;;
    esac
    ;;
  mingw*) goos="windows" ;;
  msys_nt*) goos="windows" ;;
  *) goos="${os}" ;;
  esac
  printf '%s' "${goos}"
}

get_goarch() {
  arch="$(uname -m)"
  case "${arch}" in
  aarch64) goarch="arm64" ;;
  armv*) goarch="arm" ;;
  i386) goarch="386" ;;
  i686) goarch="386" ;;
  i86pc) goarch="amd64" ;;
  x86) goarch="386" ;;
  x86_64) goarch="amd64" ;;
  *) goarch="${arch}" ;;
  esac
  printf '%s' "${goarch}"
}

check_goos_goarch() {
  case "${1}" in
  darwin/amd64) return 0 ;;
  darwin/arm64) return 0 ;;
  linux/amd64) return 0 ;;
  linux/arm64) return 0 ;;
  *)
    printf '%s: unsupported platform\n' "${1}" 1>&2
    return 1
    ;;
  esac
}

real_tag() {
  tag="${1}"
  log_debug "checking GitHub for tag ${tag}"
  release_url="${RELEASE_BASE_URL}/${tag}"
  json="$(http_get "${release_url}" "Accept: application/json")"
  if [ -z "${json}" ]; then
    log_err "real_tag error retrieving GitHub release ${tag}"
    return 1
  fi
  real_tag="$(printf '%s\n' "${json}" | tr -s '\n' ' ' | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')"
  if [ -z "${real_tag}" ]; then
    log_err "real_tag error determining real tag of GitHub release ${tag}"
    return 1
  fi
  if [ -z "${real_tag}" ]; then
    return 1
  fi
  log_debug "found tag ${real_tag} for ${tag}"
  printf '%s' "${real_tag}"
}

http_get() {
  tmpfile="$(mktemp)"
  http_download "${tmpfile}" "${1}" "${2}" || return 1
  body="$(cat "${tmpfile}")"
  rm -f "${tmpfile}"
  printf '%s\n' "${body}"
}

http_download_curl() {
  local_file="${1}"
  source_url="${2}"
  header="${3}"
  if [ -z "${header}" ]; then
    code="$(curl -w '%{http_code}' -fsSL -o "${local_file}" "${source_url}")"
  else
    code="$(curl -w '%{http_code}' -fsSL -H "${header}" -o "${local_file}" "${source_url}")"
  fi
  if [ "${code}" != "200" ]; then
    log_debug "http_download_curl received HTTP status ${code}"
    return 1
  fi
  return 0
}

http_download_wget() {
  local_file="${1}"
  source_url="${2}"
  header="${3}"
  if [ -z "${header}" ]; then
    wget -q -O "${local_file}" "${source_url}" || return 1
  else
    wget -q --header "${header}" -O "${local_file}" "${source_url}" || return 1
  fi
}

http_download() {
  log_debug "http_download ${2}"
  if is_command curl; then
    http_download_curl "${@}" || return 1
    return
  elif is_command wget; then
    http_download_wget "${@}" || return 1
    return
  fi
  log_crit "http_download unable to find wget or curl"
  return 1
}

hash_sha256() {
  target="${1}"
  if is_command sha256sum; then
    hash="$(sha256sum "${target}")" || return 1
    printf '%s' "${hash}" | cut -d ' ' -f 1
  elif is_command shasum; then
    hash="$(shasum -a 256 "${target}" 2>/dev/null)" || return 1
    printf '%s' "${hash}" | cut -d ' ' -f 1
  elif is_command sha256; then
    hash="$(sha256 -q "${target}" 2>/dev/null)" || return 1
    printf '%s' "${hash}" | cut -d ' ' -f 1
  elif is_command openssl; then
    hash="$(openssl dgst -sha256 "${target}")" || return 1
    printf '%s' "${hash}" | sed 's/.*= //'
  else
    log_crit "hash_sha256 unable to find command to compute SHA256 hash"
    return 1
  fi
}

hash_sha256_verify() {
  target="${1}"
  checksums="${2}"
  basename="${target##*/}"

  want="$(awk -v name="${basename}" '$2 == name {print $1}' "${checksums}" 2>/dev/null)"
  if [ -z "${want}" ]; then
    log_err "hash_sha256_verify unable to find checksum for ${target} in ${checksums}"
    return 1
  fi

  got="$(hash_sha256 "${target}")"
  if [ "${want}" != "${got}" ]; then
    log_err "hash_sha256_verify checksum for ${target} did not verify ${want} vs ${got}"
    return 1
  fi
}

untar() {
  tarball="${1}"
  case "${tarball}" in
  *.tar.gz | *.tgz) tar -xzf "${tarball}" ;;
  *.tar) tar -xf "${tarball}" ;;
  *.zip) unzip -- "${tarball}" ;;
  *)
    log_err "untar unknown archive format for ${tarball}"
    return 1
    ;;
  esac
}

is_command() {
  type "${1}" >/dev/null 2>&1
}

log_debug() {
  [ 3 -le "${LOG_LEVEL}" ] || return 0
  printf 'debug %s\n' "${*}" 1>&2
}

log_info() {
  [ 2 -le "${LOG_LEVEL}" ] || return 0
  printf 'info %s\n' "${*}" 1>&2
}

log_err() {
  [ 1 -le "${LOG_LEVEL}" ] || return 0
  printf 'error %s\n' "${*}" 1>&2
}

log_crit() {
  [ 0 -le "${LOG_LEVEL}" ] || return 0
  printf 'critical %s\n' "${*}" 1>&2
}

main "${@}"
