#!/usr/bin/env bash
#
# install.sh — bootstrap installer for the xmlui CLI.
#
# Usage:
#   curl -fsSL https://github.com/xmlui-org/xmlui-cli/releases/latest/download/install.sh | sh
#
# What it does:
#   1. Detects platform (uname -s/-m).
#   2. Downloads the matching release artifact and SHA256SUMS.
#   3. Verifies SHA256.
#   4. Extracts the binary.
#   5. Runs `xmlui install` to copy it onto PATH.
#
# Override the release tag with XMLUI_VERSION=v1.2.3 (default: latest).
# Override the download base URL entirely with XMLUI_BASE_URL=https://example.com
# (useful for local dry-runs against a python -m http.server).

set -euo pipefail

VERSION="${XMLUI_VERSION:-latest}"
REPO="xmlui-org/xmlui-cli"

if [[ -n "${XMLUI_BASE_URL:-}" ]]; then
  BASE_URL="${XMLUI_BASE_URL}"
elif [[ "${VERSION}" == "latest" ]]; then
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
else
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
fi

OS="$(uname -s)"
ARCH="$(uname -m)"

case "${OS}_${ARCH}" in
  Darwin_arm64)        ARTIFACT="xmlui-macos-arm64.tar.gz" ;;
  Darwin_x86_64)       ARTIFACT="xmlui-macos-intel.tar.gz" ;;
  Linux_x86_64|Linux_amd64) ARTIFACT="xmlui-linux-amd64.tar.gz" ;;
  *)
    echo "xmlui install: unsupported platform ${OS}/${ARCH}" >&2
    echo "Supported: macOS arm64/x86_64, Linux x86_64." >&2
    exit 1
    ;;
esac

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "xmlui install: missing required tool: $1" >&2
    exit 1
  }
}
require curl
require tar

# sha256 helper: prefer shasum (macOS default), fall back to sha256sum (Linux).
sha256_of() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    echo "xmlui install: neither shasum nor sha256sum is available" >&2
    exit 1
  fi
}

TMP="$(mktemp -d)"
cleanup() { rm -rf "${TMP}"; }
trap cleanup EXIT

ARTIFACT_PATH="${TMP}/${ARTIFACT}"
SUMS_PATH="${TMP}/SHA256SUMS"

echo "Downloading ${ARTIFACT}…"
curl -fsSL "${BASE_URL}/${ARTIFACT}" -o "${ARTIFACT_PATH}"

echo "Downloading SHA256SUMS…"
curl -fsSL "${BASE_URL}/SHA256SUMS" -o "${SUMS_PATH}"

EXPECTED="$(awk -v f="${ARTIFACT}" '$2 == f { print $1; exit }' "${SUMS_PATH}")"
if [[ -z "${EXPECTED}" ]]; then
  echo "xmlui install: ${ARTIFACT} not found in SHA256SUMS — refusing to install." >&2
  exit 1
fi
ACTUAL="$(sha256_of "${ARTIFACT_PATH}")"
if [[ "${ACTUAL}" != "${EXPECTED}" ]]; then
  echo "xmlui install: SHA256 mismatch for ${ARTIFACT}." >&2
  echo "  expected: ${EXPECTED}" >&2
  echo "  actual:   ${ACTUAL}" >&2
  echo "Aborting." >&2
  exit 1
fi
echo "SHA256 verified."

echo "Extracting…"
tar -xzf "${ARTIFACT_PATH}" -C "${TMP}"

BIN="$(find "${TMP}" -type f -name xmlui | head -n 1)"
if [[ -z "${BIN}" ]]; then
  echo "xmlui install: xmlui binary not found in archive" >&2
  exit 1
fi
chmod +x "${BIN}"

# Hand off to the binary's own install logic. It picks /usr/local/bin if
# writable, else ~/.local/bin, and prints the PATH hint if needed.
"${BIN}" install "$@"
