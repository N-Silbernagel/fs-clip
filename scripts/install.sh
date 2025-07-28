#!/bin/sh

# fs-clip install script
# contains code from and inspired by
# https://github.com/client9/shlib
# https://github.com/twpayne/chezmoi/

set -e

TAGARG=latest
LOG_LEVEL=2

tmpdir="$(mktemp -d)"
trap 'rm -rf -- "${tmpdir}"' EXIT
trap 'exit' INT TERM

usage() {
	this="${1}"
	cat <<EOF
${this}: download fs-clip and install it

Usage: ${this} [-d]
	-d	enables debug logging.
EOF
	exit 2
}

main() {
	parse_args "${@}"
	shift "$((OPTIND - 1))"

	GOOS="$(get_goos)"
	GOARCH="$(get_goarch)"
	check_goos_goarch "${GOOS}/${GOARCH}"

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
  	case "${GOARCH}" in
  	*) arch="${GOARCH}" ;;
  	esac

	# download tarball
	NAME="fs-clip_${GOOS}${GOOS_EXTRA}_${arch}"
	TARBALL="${NAME}.${FORMAT}"
	TARBALL_URL="https://github.com/N-Silbernagel/fs-clip/releases/download/${VERSION}/${TARBALL}"
	http_download "${tmpdir}/${TARBALL}" "${TARBALL_URL}" || exit 1

	# download checksums
	# TODO add linux checksum check
	CHECKSUMS="fs-clip_${VERSION}_checksums.txt"
	CHECKSUMS_URL="https://github.com/N-Silbernagel/fs-clip/releases/download/${VERSION}/${CHECKSUMS}"
	http_download "${tmpdir}/${CHECKSUMS}" "${CHECKSUMS_URL}" || exit 1

	# verify checksums
	hash_sha256_verify "${tmpdir}/${TARBALL}" "${tmpdir}/${CHECKSUMS}"

	(cd -- "${tmpdir}" && untar "${TARBALL}")

	# install binary
	make install
}

parse_args() {
	while getopts "b:dh?t:" arg; do
		case "${arg}" in
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
	windows/amd64) return 0 ;;
	*)
		printf '%s: unsupported platform\n' "${1}" 1>&2
		return 1
		;;
	esac
}

real_tag() {
	tag="${1}"
	log_debug "checking GitHub for tag ${tag}"
	release_url="https://github.com/N-Silbernagel/fs-clip/releases/${tag}"
	json="$(http_get "${release_url}" "Accept: application/json")"
	if [ -z "${json}" ]; then
		log_err "real_tag error retrieving GitHub release ${tag}"
		return 1
	fi
	real_tag="$(printf '%s\n' "${json}" | tr -s '\n' ' ' | sed 's/.*"tag_name":"//' | sed 's/".*//')"
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
		printf '%s' "${hash}" | cut -d ' ' -f a
	else
		log_crit "hash_sha256 unable to find command to compute SHA256 hash"
		return 1
	fi
}

hash_sha256_verify() {
	target="${1}"
	checksums="${2}"
	basename="${target##*/}"

	want="$(grep -i "${basename}" "${checksums}" 2>/dev/null | tr '\t' ' ' | cut -d ' ' -f 1)"
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
