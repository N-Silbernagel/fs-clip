#!/bin/sh
set -eu

RELEASE_BASE_URL=${FS_CLIP_RELEASE_BASE_URL:-https://github.com/N-Silbernagel/fs-clip/releases}
BINDIR=
TAG=latest
while getopts 'b:t:dh' option; do
  case "$option" in
    b) BINDIR=$OPTARG ;;
    t) TAG=$OPTARG ;;
    d) set -x ;;
    h) echo 'Usage: install.sh [-b binary-directory] [-t release-tag]'; exit 0 ;;
    *) exit 2 ;;
  esac
done
shift "$((OPTIND - 1))"
[ "$#" -eq 0 ] || { echo 'Unexpected installer arguments' >&2; exit 2; }

case "$(uname -s)" in
  Darwin) platform=darwin ;;
  Linux) platform=linux ;;
  *) echo 'Supported platforms: macOS and Linux' >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo 'Supported architectures: AMD64 and ARM64' >&2; exit 1 ;;
esac

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT
trap 'exit 1' INT TERM

download() {
  curl -fsSL --retry 3 --connect-timeout 10 --max-time 120 "$1" -o "$2"
}

curl -fsSL --retry 3 --connect-timeout 10 --max-time 120 -H 'Accept: application/json' \
  "$RELEASE_BASE_URL/$TAG" -o "$workdir/release.json"
TAG=$(tr '\n' ' ' < "$workdir/release.json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
case "$TAG" in
  ''|*[!a-zA-Z0-9._-]*) echo 'Unable to determine release tag' >&2; exit 1 ;;
esac
version=${TAG#v}
archive="fs-clip_${version}_${platform}_${arch}.tar.gz"
download "$RELEASE_BASE_URL/download/$TAG/$archive" "$workdir/$archive"
download "$RELEASE_BASE_URL/download/$TAG/fs-clip_${version}_checksums.txt" "$workdir/checksums.txt"
want=$(awk -v name="$archive" '$2 == name {print $1}' "$workdir/checksums.txt")
case "$want" in
  ''|*[!a-fA-F0-9]*) echo 'Missing or invalid archive checksum' >&2; exit 1 ;;
esac
[ "${#want}" -eq 64 ] || { echo 'Invalid archive checksum' >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$workdir/$archive" | cut -d ' ' -f 1)
elif command -v shasum >/dev/null 2>&1; then
  got=$(shasum -a 256 "$workdir/$archive" | cut -d ' ' -f 1)
else
  echo 'Install sha256sum or shasum to verify the download' >&2
  exit 1
fi
want=$(printf '%s' "$want" | tr '[:upper:]' '[:lower:]')
[ "$want" = "$got" ] || { echo 'Archive checksum did not verify' >&2; exit 1; }
tar -xzf "$workdir/$archive" -C "$workdir"
chmod 0755 "$workdir/fs-clip"
if [ -n "$BINDIR" ]; then
  "$workdir/fs-clip" install --bin-dir "$BINDIR"
else
  "$workdir/fs-clip" install
fi
