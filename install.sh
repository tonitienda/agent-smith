#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-tonitienda/agent-smith}"
BINARY="${BINARY:-smith}"
VERSION_INPUT="${1:-latest}"

log() {
  printf '%s\n' "$*" >&2
}

fail() {
  log "install.sh: $*"
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf 'linux' ;;
    Darwin) printf 'darwin' ;;
    *) fail "unsupported operating system: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

resolve_version() {
  if [ "${VERSION_INPUT}" != "latest" ]; then
    printf '%s' "${VERSION_INPUT}"
    return
  fi

  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n 1
}

checksum_verify() {
  checksum_file="$1"
  archive_name="$2"

  if command -v sha256sum >/dev/null 2>&1; then
    (
      cd "$(dirname "${checksum_file}")"
      sha256sum -c --ignore-missing "$(basename "${checksum_file}")"
    )
    return
  fi

  if command -v shasum >/dev/null 2>&1; then
    expected="$(grep " ${archive_name}\$" "${checksum_file}" | awk '{print $1}')"
    [ -n "${expected}" ] || fail "missing checksum entry for ${archive_name}"
    actual="$(shasum -a 256 "$(dirname "${checksum_file}")/${archive_name}" | awk '{print $1}')"
    [ "${expected}" = "${actual}" ] || fail "checksum mismatch for ${archive_name}"
    return
  fi

  log "warning: sha256sum/shasum not found; skipping checksum verification"
}

main() {
  need_cmd curl
  need_cmd tar
  need_cmd install
  need_cmd mktemp

  os="$(detect_os)"
  arch="$(detect_arch)"
  version="$(resolve_version)"
  [ -n "${version}" ] || fail "could not resolve release version"

  archive_name="${BINARY}_${version#v}_${os}_${arch}.tar.gz"
  base_url="https://github.com/${REPO}/releases/download/${version}"
  archive_url="${base_url}/${archive_name}"
  checksum_name="${BINARY}_${version#v}_checksums.txt"
  checksum_url="${base_url}/${checksum_name}"

  install_dir="${INSTALL_DIR:-}"
  if [ -z "${install_dir}" ]; then
    if [ -w "/usr/local/bin" ]; then
      install_dir="/usr/local/bin"
    else
      install_dir="${HOME}/.local/bin"
    fi
  fi

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "${tmpdir}"' EXIT

  archive_path="${tmpdir}/${archive_name}"
  checksum_path="${tmpdir}/${checksum_name}"

  log "Downloading ${BINARY} ${version} for ${os}/${arch}"
  curl -fsSL "${archive_url}" -o "${archive_path}"
  curl -fsSL "${checksum_url}" -o "${checksum_path}"
  checksum_verify "${checksum_path}" "${archive_name}"

  tar -xzf "${archive_path}" -C "${tmpdir}"
  mkdir -p "${install_dir}"
  install -m 0755 "${tmpdir}/${BINARY}" "${install_dir}/${BINARY}"

  log "Installed ${BINARY} ${version} to ${install_dir}/${BINARY}"
  case ":${PATH}:" in
    *:"${install_dir}":*) ;;
    *)
      log "warning: ${install_dir} is not on PATH"
      ;;
  esac
}

main "$@"
