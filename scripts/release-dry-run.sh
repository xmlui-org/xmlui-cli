#!/usr/bin/env bash
#
# release-dry-run.sh — build a local-platform release artifact + SHA256SUMS,
# then print exactly what to do next to exercise install.sh end-to-end.
#
# Tier 1 + Tier 2 of the release dry-run process described in
# claude-config-plan.md. Scoped to the host platform only — does not exercise
# the full CI matrix. CI is the only place that proves the cross-compile
# matrix works; this script proves install.sh works on this machine.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${REPO_ROOT}/release-dry-run"

OS="$(uname -s)"
ARCH="$(uname -m)"

case "${OS}_${ARCH}" in
  Darwin_arm64)        ARTIFACT="xmlui-macos-arm64.tar.gz" ; GOOS=darwin  ; GOARCH=arm64 ;;
  Darwin_x86_64)       ARTIFACT="xmlui-macos-intel.tar.gz" ; GOOS=darwin  ; GOARCH=amd64 ;;
  Linux_x86_64|Linux_amd64) ARTIFACT="xmlui-linux-amd64.tar.gz" ; GOOS=linux ; GOARCH=amd64 ;;
  *)
    echo "release-dry-run: unsupported host platform ${OS}/${ARCH}" >&2
    exit 1
    ;;
esac

# Pick the sha256 binary the same way install.sh does so we match what the
# user will actually run.
sha256_of() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    sha256sum "$1" | awk '{print $1}'
  fi
}

mkdir -p "${OUT_DIR}"
rm -f "${OUT_DIR}/xmlui" "${OUT_DIR}/${ARTIFACT}" "${OUT_DIR}/SHA256SUMS"

echo "[1/3] Building xmlui (${GOOS}/${GOARCH})…"
( cd "${REPO_ROOT}" && GOOS="${GOOS}" GOARCH="${GOARCH}" go build -o "${OUT_DIR}/xmlui" . )

echo "[2/3] Packaging ${ARTIFACT}…"
( cd "${OUT_DIR}" && tar -czf "${ARTIFACT}" xmlui && rm xmlui )

echo "[3/3] Generating SHA256SUMS…"
( cd "${OUT_DIR}"
  {
    printf "%s  %s\n" "$(sha256_of "${ARTIFACT}")" "${ARTIFACT}"
    printf "%s  install.sh\n" "$(sha256_of "${REPO_ROOT}/install.sh")"
    printf "%s  install.ps1\n" "$(sha256_of "${REPO_ROOT}/install.ps1")"
  } > SHA256SUMS
)

cat <<EOF

Done. Artifacts in ${OUT_DIR}:
$(ls -la "${OUT_DIR}")

SHA256SUMS:
$(cat "${OUT_DIR}/SHA256SUMS")

To exercise install.sh end-to-end:

  # Terminal A — serve the artifacts
  cd ${OUT_DIR} && python3 -m http.server 8000

  # Terminal B — run the installer with the override
  XMLUI_BASE_URL=http://localhost:8000 \\
    sh ${REPO_ROOT}/install.sh --prefix /tmp/xmlui-install

  # Verify
  /tmp/xmlui-install/xmlui --version
  /tmp/xmlui-install/xmlui doctor

  # Cleanup
  rm -rf /tmp/xmlui-install ${OUT_DIR}
EOF
